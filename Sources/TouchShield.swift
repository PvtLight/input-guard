import AppKit

// Touches on a touch screen (built in, external, or a Sidecar iPad) never
// reach the session event tap: macOS hands each touch to the window under the
// finger, and AppKit turns it into clicks inside that app. While guarded, a
// clear window above every other window covers each screen, so touches land
// on Input Guard and do nothing. It never becomes key or activates the app, so
// focus stays where it was. It is only a window: when Input Guard quits,
// crashes, or is ended by its recovery helper, macOS removes it.
final class TouchShield {
    private final class Panel: NSPanel {
        override var canBecomeKey: Bool { false }
        override var canBecomeMain: Bool { false }
    }
    private var panels: [Panel] = []
    private var frames: [NSRect] = []

    // Stays true while screens come and go, so the health check keeps raising it.
    private(set) var isRaised = false
    var windowNumbers: [Int] { panels.map(\.windowNumber) }

    // Covers every screen. The health check calls it again, so the shield
    // follows display changes and returns to the front if a window got above it.
    func raise() {
        isRaised = true
        let screens = NSScreen.screens.map(\.frame)
        var replaced: [Panel] = []
        if screens != frames {
            replaced = panels
            frames = screens
            panels = screens.map(Self.panel)
        }
        for panel in panels where !covers(panel) { panel.orderFrontRegardless() }
        // Only after the new ones are up, so no touch slips through a display change.
        replaced.forEach { $0.orderOut(nil) }
    }

    func lower() {
        isRaised = false
        panels.forEach { $0.orderOut(nil) }
        panels = []; frames = []
    }

    private func covers(_ panel: Panel) -> Bool {
        let middle = NSPoint(x: panel.frame.midX, y: panel.frame.midY)
        return panel.isVisible && NSWindow.windowNumber(at: middle, belowWindowWithWindowNumber: 0) == panel.windowNumber
    }

    private static func panel(_ frame: NSRect) -> Panel {
        let panel = Panel(contentRect: frame, styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
        panel.setFrame(frame, display: false)
        panel.level = NSWindow.Level(rawValue: Int(CGShieldingWindowLevel()))
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary, .ignoresCycle]
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = false
        // A clear window lets clicks and touches fall through unless told otherwise.
        panel.ignoresMouseEvents = false
        panel.canHide = false
        panel.isReleasedWhenClosed = false
        panel.isRestorable = false
        panel.animationBehavior = .none
        panel.setAccessibilityElement(false)
        // macOS 27: touches that start here can't begin system gestures such as
        // edge swipes. The macOS 26 SDK this builds with doesn't declare it yet.
        if panel.responds(to: NSSelectorFromString("setDisablesDirectTouchSystemGestures:")) {
            panel.setValue(true, forKey: "disablesDirectTouchSystemGestures")
        }
        return panel
    }
}

// Raises the shield for about a second, with no input tap, and asks macOS
// which window a click or touch would reach on a grid over every screen. Only
// the camera notch and the rounded display corners, which have no pixels, are
// skipped. A finger is still needed to try real touches.
func runTouchShieldCheck() throws {
    func check(_ ok: @autoclosure () -> Bool, _ name: String) throws {
        if !ok() { throw GuardError.message("FAILED: \(name)") }
        print("PASS: \(name)")
    }
    let shield = TouchShield()
    shield.raise()
    defer { shield.lower() }
    RunLoop.current.run(until: Date().addingTimeInterval(0.2))
    let numbers = shield.windowNumbers
    try check(numbers.count == NSScreen.screens.count, "One shield window per screen (\(numbers.count))")
    var points = 0, misses = 0
    for (screen, number) in zip(NSScreen.screens, numbers) {
        let frame = screen.frame
        var notch = NSRect.zero
        if let left = screen.auxiliaryTopLeftArea, let right = screen.auxiliaryTopRightArea {
            notch = NSRect(x: left.maxX, y: frame.maxY - screen.safeAreaInsets.top,
                           width: right.minX - left.maxX, height: screen.safeAreaInsets.top)
        }
        // Edge to edge: every 16 points across, rows every 4 points over the
        // menu bar and every 24 below it.
        let columns = Array(stride(from: frame.minX + 1, to: frame.maxX - 1, by: 16)) + [frame.maxX - 1]
        var rows: [CGFloat] = []
        var y = frame.maxY - 1
        while y > frame.minY + 1 { rows.append(y); y -= y > frame.maxY - 44 ? 4 : 24 }
        rows.append(frame.minY + 1)
        for y in rows {
            for x in columns {
                let point = NSPoint(x: x, y: y)
                let corner = min(x - frame.minX, frame.maxX - x) < 12 && min(y - frame.minY, frame.maxY - y) < 12
                if corner || notch.contains(point) { continue }
                points += 1
                let hit = NSWindow.windowNumber(at: point, belowWindowWithWindowNumber: 0)
                guard hit != number else { continue }
                misses += 1
                if misses <= 5 {
                    let info = (CGWindowListCopyWindowInfo([.optionIncludingWindow], CGWindowID(hit)) as? [[String: Any]])?.first ?? [:]
                    print("   (\(point.x), \(point.y)) on \(screen.localizedName) reaches \(info[kCGWindowOwnerName as String] ?? "no window"), level \(info[kCGWindowLayer as String] ?? "?")")
                }
            }
        }
    }
    try check(misses == 0, "\(points - misses) of \(points) click and touch points on \(numbers.count) screen(s) reach the shield, menu bar and Dock included")
    shield.raise()
    try check(shield.windowNumbers == numbers, "Raising again over the same screens keeps the same windows")
    let gestures = NSApp.windows.filter { numbers.contains($0.windowNumber) }
        .filter { $0.responds(to: NSSelectorFromString("disablesDirectTouchSystemGestures")) }
    try check(gestures.allSatisfy { $0.value(forKey: "disablesDirectTouchSystemGestures") as? Bool == true },
              gestures.isEmpty ? "System touch gestures: not offered by this macOS" : "System touch gestures are off over the shield")
    try check(NSApp.keyWindow == nil && !NSApp.isActive, "The shield takes no keyboard focus and does not activate Input Guard")
    shield.lower()
    RunLoop.current.run(until: Date().addingTimeInterval(0.1))
    let middles = NSScreen.screens.map { NSPoint(x: $0.frame.midX, y: $0.frame.midY) }
    try check(!shield.isRaised && middles.allSatisfy { !numbers.contains(NSWindow.windowNumber(at: $0, belowWindowWithWindowNumber: 0)) },
              "Lowering the shield gives clicks and touches back to the apps below")
}
