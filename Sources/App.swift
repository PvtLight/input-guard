import AppKit
import ApplicationServices
import LocalAuthentication
import IOKit.pwr_mgt

final class SurfaceView: NSView {
    override func draw(_ dirtyRect: NSRect) {
        Brand.canvas.setFill()
        dirtyRect.fill()
    }
}

final class FlippedView: NSView {
    override var isFlipped: Bool { true }
}

final class GuardApp: NSObject, NSApplicationDelegate, NSWindowDelegate {
    private var status: NSStatusItem!
    private var setup: NSWindow?
    private var panel: NSPanel?
    private var panelTitle: NSTextField?
    private var pagePicker: NSSegmentedControl?
    private var pageContainer: NSStackView?
    private var overviewPage: NSStackView?
    private var settingsPage: NSStackView?
    private var startButton: NSButton?
    private var accessibilityStatus: NSTextField?
    private var accessibilityButton: NSButton?
    private var passphraseFeedback: NSTextField?
    private var note: NSTextField?
    private var dots: NSTextField?
    private var passwordField: NSSecureTextField!
    private var confirmationField: NSSecureTextField!
    private var oldPasswordField: NSSecureTextField!
    private var automationCheckbox: NSButton!
    private var message: NSTextField!
    private var record: PasswordRecord?
    private var tap: CFMachPort?
    private var source: CFRunLoopSource?
    private var watchdog: Timer?
    private var testDeadline: TimeInterval?
    private var recovery: RecoverySupervisor?
    private var hardDeadline: TimeInterval?
    private var assertion: IOPMAssertionID = 0
    private var hasAssertion = false
    private var displayAssertion: IOPMAssertionID = 0
    private var hasDisplayAssertion = false
    private var locked = false
    private var enteringPassword = false
    private var prompt = UnlockPrompt()
    private var verifying = false
    private var limiter = AttemptLimiter()
    private var auth: LAContext?
    private var session = UUID()
    private var lastPrompt: TimeInterval = 0
    private var tapInterrupted = false
    private var filter = TapFilter(held: [])
    private let touchShield = TouchShield()
    private var globalHotKey: GlobalHotKey?
    private var shortcutStatus = "Shortcut: Control–Option–Command–L locks input, or opens the unlock prompt while guarded. Input Guard must be running."
    private var activationPending = false
    private var sessionResetObserver: SessionResetObserver?
    private var allowAutomation: Bool { UserDefaults.standard.bool(forKey: "allowAutomation") }
    private var now: TimeInterval { RecoveryClock.now }

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        if CommandLine.arguments.contains("--preview"), CommandLine.arguments.contains("--preview-light") {
            NSApp.appearance = NSAppearance(named: .aqua)
        }
        status = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        let shortcut = GlobalHotKey { [weak self] in self?.activateShortcut() }
        do { try shortcut.register(); globalHotKey = shortcut }
        catch { shortcutStatus = error.localizedDescription }
        updateMenu()
        showSetup()
        if let index = CommandLine.arguments.firstIndex(of: "--preview"), CommandLine.arguments.count > index + 1 {
            let destination = CommandLine.arguments[index + 1]
            let previewPanel = CommandLine.arguments.contains("--preview-notice") || CommandLine.arguments.contains("--preview-unlock")
            if previewPanel {
                setup?.orderOut(nil)
                if CommandLine.arguments.contains("--preview-unlock") {
                    // The longest prompt message, with Caps Lock on.
                    var limiter = AttemptLimiter()
                    for _ in 0..<5 { limiter.failed(at: now) }
                    enteringPassword = true
                    prompt = UnlockPrompt(capsLock: true)
                    prompt.type("example")
                    _ = prompt.submit(limiter, now: now)
                }
                showPanel(unlock: enteringPassword) // Visual preview only: no input tap or helper.
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { [weak self] in
                let previewView = previewPanel ? self?.panel?.contentView : self?.setup?.contentView
                guard let view = previewView,
                      let bitmap = view.bitmapImageRepForCachingDisplay(in: view.bounds) else { exit(1) }
                view.display()
                view.cacheDisplay(in: view.bounds, to: bitmap)
                guard let png = bitmap.representation(using: .png, properties: [:]) else { exit(1) }
                do { try png.write(to: URL(fileURLWithPath: destination)) }
                catch { fputs("Preview failed: \(error)\n", stderr); exit(1) }
                NSApp.terminate(nil)
            }
        }
        sessionResetObserver = SessionResetObserver { [weak self] in self?.sessionDidReset() }
        if CommandLine.arguments.contains("--startup-check") {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { [self] in
                let inactive = !locked && tap == nil && source == nil && recovery == nil
                    && watchdog == nil && !touchShield.isRaised
                    && hardDeadline == nil && testDeadline == nil && !activationPending
                    && !hasAssertion && !hasDisplayAssertion
                let noRestoration = !applicationShouldSaveSecureApplicationState(NSApp)
                    && !applicationShouldRestoreSecureApplicationState(NSApp)
                    && setup?.isRestorable == false
                guard inactive && noRestoration else {
                    endLock()
                    fputs("FAIL: fresh launch must remain inactive with restoration disabled.\n", stderr)
                    exit(1)
                }
                print("PASS: fresh launch is unlocked, with no input tap, touch shield, helper, or sleep assertions.")
                print("PASS: saved-state restoration is disabled; manual activation is required.")
                NSApp.terminate(nil)
            }
        }
    }

    // Never save or restore an activation or guarded session.
    func applicationShouldSaveSecureApplicationState(_ app: NSApplication) -> Bool { false }
    func applicationShouldRestoreSecureApplicationState(_ app: NSApplication) -> Bool { false }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        if locked { showUnlock(); return .terminateCancel }
        return .terminateNow
    }

    func applicationWillTerminate(_ notification: Notification) {
        globalHotKey?.unregister()
        sessionResetObserver = nil
    }

    func applicationDidBecomeActive(_ notification: Notification) {
        refreshAccessibilityStatus()
    }

    func windowDidBecomeKey(_ notification: Notification) {
        refreshAccessibilityStatus()
    }

    private func refreshAccessibilityStatus() {
        let enabled = AXIsProcessTrusted()
        accessibilityStatus?.stringValue = enabled
            ? "✓ Accessibility is already enabled."
            : "Enable Input Guard in macOS Accessibility settings."
        accessibilityButton?.title = enabled ? "Review Accessibility Settings…" : "Open Accessibility Settings…"
    }

    private func activateShortcut() {
        guard !activationPending, NSApp.modalWindow == nil else { return }
        if locked { showUnlock(); return }
        beginLock(test: false)
    }

    private func updateMenu() {
        status.button?.image = NSImage(systemSymbolName: locked ? "lock.fill" : "lock.open", accessibilityDescription: "Input Guard")
        status.button?.title = locked ? (testDeadline == nil ? " Agent working" : " TEST") : ""
        status.button?.toolTip = locked ? "Agent working · ⌃⌥⌘L to resume input" : "Input Guard · ⌃⌥⌘L to start"
        let menu = NSMenu()
        let lockItem = menu.addItem(withTitle: locked ? "Unlock…" : "Lock Input", action: #selector(lockOrUnlock), keyEquivalent: "")
        menu.autoenablesItems = false
        lockItem.isEnabled = true
        startButton?.isEnabled = !locked
        if globalHotKey != nil {
            lockItem.keyEquivalent = "l"
            lockItem.keyEquivalentModifierMask = [.control, .option, .command]
        }
        if !locked {
            menu.addItem(withTitle: "Optional 15-Second Trial", action: #selector(testLock), keyEquivalent: "")
            menu.addItem(.separator())
            menu.addItem(withTitle: "Open Input Guard…", action: #selector(showSetup), keyEquivalent: "")
            menu.addItem(withTitle: "Quit Input Guard", action: #selector(quit), keyEquivalent: "")
        }
        for item in menu.items { item.target = self }
        status.menu = menu
    }

    private func label(_ text: String, size: CGFloat = 13, weight: NSFont.Weight = .regular) -> NSTextField {
        let label = NSTextField(wrappingLabelWithString: text)
        label.font = .systemFont(ofSize: size, weight: weight)
        return label
    }

    private func button(_ title: String, _ action: Selector) -> NSButton {
        let button = NSButton(title: title, target: self, action: action)
        button.bezelStyle = .rounded
        return button
    }

    private func column(spacing: CGFloat = 10) -> NSStackView {
        let stack = NSStackView()
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = spacing
        stack.translatesAutoresizingMaskIntoConstraints = false
        return stack
    }

    private func addFullWidth(_ view: NSView, to stack: NSStackView) {
        view.translatesAutoresizingMaskIntoConstraints = false
        stack.addArrangedSubview(view)
        view.widthAnchor.constraint(equalTo: stack.widthAnchor).isActive = true
    }

    private func secondary(_ text: String, size: CGFloat = 12) -> NSTextField {
        let field = label(text, size: size)
        field.textColor = .secondaryLabelColor
        return field
    }

    private func card(_ title: String, subtitle: String? = nil, content: [NSView]) -> NSBox {
        let box = NSBox()
        box.boxType = .custom
        box.titlePosition = .noTitle
        box.borderWidth = 0
        box.cornerRadius = 12
        box.fillColor = Brand.cardBackground
        box.contentViewMargins = .zero
        let stack = column(spacing: 10)
        addFullWidth(label(title, size: 14, weight: .semibold), to: stack)
        if let subtitle { addFullWidth(secondary(subtitle), to: stack) }
        for view in content {
            if view is NSTextField || view is NSStackView { addFullWidth(view, to: stack) }
            else { stack.addArrangedSubview(view) }
        }
        box.contentView!.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: box.contentView!.leadingAnchor, constant: 18),
            stack.trailingAnchor.constraint(equalTo: box.contentView!.trailingAnchor, constant: -18),
            stack.topAnchor.constraint(equalTo: box.contentView!.topAnchor, constant: 16),
            stack.bottomAnchor.constraint(equalTo: box.contentView!.bottomAnchor, constant: -16)
        ])
        return box
    }

    @objc private func changePage() {
        guard let container = pageContainer,
              let page = pagePicker?.selectedSegment == 1 ? settingsPage : overviewPage else { return }
        if container.arrangedSubviews.first === page { return }
        for existing in container.arrangedSubviews {
            container.removeArrangedSubview(existing)
            existing.removeFromSuperview()
        }
        addFullWidth(page, to: container)
        setup?.contentView?.layoutSubtreeIfNeeded()
        if let scroll = setup?.contentView?.subviews.first as? NSScrollView {
            scroll.contentView.scroll(to: .zero)
        }
    }

    @objc private func showSetup() {
        guard !locked else { showUnlock(); return }
        if let setup { refreshAccessibilityStatus(); setup.makeKeyAndOrderFront(nil); NSApp.activate(ignoringOtherApps: true); return }
        let height = min(CGFloat(750), (NSScreen.main?.visibleFrame.height ?? 840) - 80)
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 580, height: height),
                              styleMask: [.titled, .closable], backing: .buffered, defer: false)
        window.title = "Input Guard"
        window.titlebarAppearsTransparent = true
        window.backgroundColor = Brand.canvas
        window.contentView = SurfaceView(frame: NSRect(x: 0, y: 0, width: 580, height: height))
        window.isReleasedWhenClosed = false
        window.isRestorable = false
        window.delegate = self
        let stack = column(spacing: 16)
        let scroll = NSScrollView(frame: window.contentView!.bounds)
        scroll.autoresizingMask = [.width, .height]
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        let document = FlippedView()
        document.translatesAutoresizingMaskIntoConstraints = false
        document.addSubview(stack)
        scroll.documentView = document
        window.contentView!.addSubview(scroll)
        NSLayoutConstraint.activate([
            document.widthAnchor.constraint(equalTo: scroll.contentView.widthAnchor),
            stack.leadingAnchor.constraint(equalTo: document.leadingAnchor, constant: 28),
            stack.trailingAnchor.constraint(equalTo: document.trailingAnchor, constant: -28),
            stack.topAnchor.constraint(equalTo: document.topAnchor, constant: 20),
            stack.bottomAnchor.constraint(equalTo: document.bottomAnchor, constant: -24)
        ])
        let heading = column(spacing: 4)
        heading.addArrangedSubview(label("Input Guard", size: 26, weight: .semibold))
        heading.addArrangedSubview(secondary("Your work keeps running.", size: 13))
        let header = NSStackView(views: [Brand.imageView(size: 64), heading])
        header.spacing = 14
        header.alignment = .centerY
        stack.addArrangedSubview(header)
        let picker = NSSegmentedControl(labels: ["Overview", "Settings"], trackingMode: .selectOne, target: self, action: #selector(changePage))
        let previewSettings = CommandLine.arguments.contains("--preview") && CommandLine.arguments.contains("--preview-settings")
        picker.selectedSegment = previewSettings ? 1 : 0
        picker.segmentStyle = .texturedRounded
        pagePicker = picker
        addFullWidth(picker, to: stack)

        let overview = column(spacing: 14)
        let settings = column(spacing: 14)
        overviewPage = overview
        settingsPage = settings
        let pages = column(spacing: 0)
        pageContainer = pages
        addFullWidth(pages, to: stack)
        addFullWidth(previewSettings ? settings : overview, to: pages)

        let shortcut = label("⌃ ⌥ ⌘ L", size: 23, weight: .medium)
        let shortcutHelp = secondary("Lock input now. Use it again to enter your unlock code.")
        addFullWidth(card("Your shortcut", content: [shortcut, shortcutHelp]), to: overview)
        // Registration failures remain visible; normal instructions fit in the shortcut card.
        if globalHotKey == nil { addFullWidth(secondary(shortcutStatus), to: overview) }
        let accessStatus = secondary("")
        let accessButton = button("Open Accessibility Settings…", #selector(requestAccess))
        accessibilityStatus = accessStatus
        accessibilityButton = accessButton
        refreshAccessibilityStatus()
        addFullWidth(card("Accessibility", content: [accessStatus, accessButton]), to: overview)
        let start = button("Lock Input", #selector(lockOrUnlock))
        start.controlSize = .regular
        startButton = start
        addFullWidth(card("Ready when you are", subtitle: "Locks immediately. Resume input with your passphrase or Touch ID.", content: [start]), to: overview)
        message = secondary("Use Lock Input or ⌃⌥⌘L to begin. The 15-second trial is optional.")
        addFullWidth(message, to: stack)
        addFullWidth(secondary("Always starts unlocked · No automatic startup", size: 11), to: overview)

        let passwordFeedback = secondary("Choose a separate code, not your Mac password. It stays on this Mac.")
        passphraseFeedback = passwordFeedback
        var passwordViews: [NSView] = [passwordFeedback]
        for (title, key) in [("Current passphrase · only needed to change it", 0), ("New passphrase · at least 4 characters", 1), ("Confirm new passphrase", 2)] {
            let field = NSSecureTextField()
            field.placeholderString = title
            field.setAccessibilityLabel(title)
            field.controlSize = .regular
            field.font = .systemFont(ofSize: 13)
            passwordViews.append(field)
            if key == 0 { oldPasswordField = field }
            if key == 1 { passwordField = field }
            if key == 2 { confirmationField = field }
        }
        passwordViews.append(button("Save Passphrase", #selector(savePassword)))
        addFullWidth(card("Your passphrase", content: passwordViews), to: settings)
        automationCheckbox = NSButton(checkboxWithTitle: "Allow automation events", target: self, action: #selector(changeAutomation))
        automationCheckbox.state = allowAutomation ? .on : .off
        addFullWidth(card("Automation · experimental", subtitle: "Allows input marked as software. Keyboard remappers may also pass through, and the touch screen is left unblocked so automated clicks can reach apps. Test your setup before relying on it.", content: [automationCheckbox]), to: settings)
        addFullWidth(card("Built-in recovery", content: [
            secondary("No time limit in normal sessions. Resume with your passphrase or Touch ID, or lock and unlock macOS with ⌃⌘Q."),
            secondary("The trial releases input after 15 seconds. If Input Guard freezes, a separate recovery process releases input."),
            secondary("Your desktop stays visible. Input Guard is a convenience tool; use the macOS lock for privacy."),
            button("Optional 15-Second Trial", #selector(testLock))
        ]), to: settings)
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
        addFullWidth(secondary("Input Guard  \(version)  ·  Made for Kai", size: 11), to: settings)
        setup = window
        window.contentView?.layoutSubtreeIfNeeded()
        scroll.contentView.scroll(to: .zero)
        window.center()
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func windowWillClose(_ notification: Notification) {
        if let window = notification.object as? NSWindow, window === setup {
            passwordField.stringValue = ""
            confirmationField.stringValue = ""
            oldPasswordField.stringValue = ""
            setup = nil
        }
    }

    @objc private func requestAccess() {
        if !AXIsProcessTrusted() {
            let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary
            _ = AXIsProcessTrustedWithOptions(options)
        }
        refreshAccessibilityStatus()
        NSWorkspace.shared.open(URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility")!)
    }

    @objc private func savePassword() {
        guard !locked else { return }
        do {
            let existing = try Vault.read()
            if let existing, !existing.matches(oldPasswordField.stringValue) {
                throw GuardError.message("Enter your current Input Guard passphrase to change it.")
            }
            guard passwordField.stringValue == confirmationField.stringValue else {
                throw GuardError.message("The new passphrases do not match.")
            }
            let saved = try PasswordRecord.create(passwordField.stringValue)
            try Vault.save(saved)
            passwordField.stringValue = ""
            confirmationField.stringValue = ""
            oldPasswordField.stringValue = ""
            let confirmation = existing == nil ? "Passphrase saved." : "Passphrase reset."
            passphraseFeedback?.stringValue = "✓ " + confirmation
            message.stringValue = confirmation + " You can use Lock Input whenever you’re ready."
        } catch {
            passphraseFeedback?.stringValue = error.localizedDescription
            message.stringValue = error.localizedDescription
        }
    }

    @objc private func changeAutomation() {
        guard !locked else { return }
        UserDefaults.standard.set(automationCheckbox.state == .on, forKey: "allowAutomation")
    }

    @objc private func lockOrUnlock() { if locked { showUnlock() } else { beginLock(test: false) } }
    @objc private func testLock() { beginLock(test: true) }
    @objc private func quit() { NSApp.terminate(nil) }

    private func beginLock(test: Bool) {
        guard !locked, !activationPending else { return }
        guard AXIsProcessTrusted() else {
            showSetup()
            refreshAccessibilityStatus()
            message.stringValue = "Enable Accessibility for Input Guard before starting."
            return
        }
        activationPending = true
        defer { activationPending = false }
        startGuard(test: test)
    }

    private func startGuard(test: Bool) {
        guard !locked else { return }
        do {
            guard AXIsProcessTrusted() else { throw GuardError.message("Allow Input Guard in Accessibility first, then try again.") }
            let activationSession = session
            record = try Vault.read()
            // A system lock/user switch during a Keychain prompt cancels this activation.
            guard session == activationSession else { record = nil; return }
            guard record != nil else { throw GuardError.message("Set your Input Guard passphrase in Settings before starting.") }
            let supervisor = RecoverySupervisor()
            try supervisor.start(seconds: test ? 15 : nil)
            recovery = supervisor
            hardDeadline = supervisor.deadline
            // Include gesture/system-defined event types as well as ordinary input.
            // This remains a session tap: OS-reserved controls are not guaranteed blocked.
            guard let created = CGEvent.tapCreate(tap: .cgSessionEventTap, place: .headInsertEventTap,
                options: .defaultTap, eventsOfInterest: CGEventMask.max, callback: { _, type, event, refcon in
                    guard let refcon else { return Unmanaged.passUnretained(event) }
                    return Unmanaged<GuardApp>.fromOpaque(refcon).takeUnretainedValue().handle(type, event)
                }, userInfo: Unmanaged.passUnretained(self).toOpaque()) else {
                throw GuardError.message("macOS refused the input blocker. Check Accessibility and Input Monitoring permissions, then relaunch Input Guard.")
            }
            tap = created
            guard let runSource = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, created, 0) else {
                throw GuardError.message("Could not start the input blocker.")
            }
            source = runSource
            CFRunLoopAddSource(CFRunLoopGetMain(), runSource, .commonModes)
            CGEvent.tapEnable(tap: created, enable: true)
            guard CGEvent.tapIsEnabled(tap: created) else { throw GuardError.message("The input blocker did not activate.") }
            // Apps already saw any modifiers held now, such as the shortcut's
            // Control–Option–Command. Read after enabling: the tap's callback
            // runs on this thread, so no event is judged against older state.
            filter = TapFilter(held: CGEventSource.flagsState(.combinedSessionState))
            // Touches never reach the tap, so cover the screens. Not in automation
            // mode: the cover would also catch the automated clicks it allows.
            if !allowAutomation { touchShield.raise() }
            let result = IOPMAssertionCreateWithName(kIOPMAssertionTypeNoIdleSleep as CFString,
                IOPMAssertionLevel(kIOPMAssertionLevelOn), "Input Guard keeps ongoing work running" as CFString, &assertion)
            guard result == kIOReturnSuccess else { throw GuardError.message("Could not keep the Mac awake. Lock was not started.") }
            hasAssertion = true
            let displayResult = IOPMAssertionCreateWithName(kIOPMAssertionTypeNoDisplaySleep as CFString,
                IOPMAssertionLevel(kIOPMAssertionLevelOn), "Input Guard keeps the active desktop visible" as CFString, &displayAssertion)
            guard displayResult == kIOReturnSuccess else { throw GuardError.message("Could not keep the display awake. Lock was not started.") }
            hasDisplayAssertion = true
            locked = true
            session = UUID()
            testDeadline = test ? hardDeadline : nil
            limiter = AttemptLimiter()
            tapInterrupted = false
            setup?.close()
            updateMenu()
            showPanel(unlock: false)
            let timer = Timer(timeInterval: 0.25, repeats: true) { [weak self] _ in self?.checkHealth() }
            watchdog = timer
            RunLoop.main.add(timer, forMode: .common)
        } catch {
            endLock()
            showSetup()
            message.stringValue = error.localizedDescription
        }
    }

    private func handle(_ type: CGEventType, _ event: CGEvent) -> Unmanaged<CGEvent>? {
        if type == .tapDisabledByTimeout || type == .tapDisabledByUserInput {
            // Never re-arm a tap macOS disabled. Give control back immediately.
            tapInterrupted = true
            DispatchQueue.main.async { [weak self] in self?.checkHealth() }
            return Unmanaged.passUnretained(event)
        }
        guard locked, !tapInterrupted else { return Unmanaged.passUnretained(event) }
        if enteringPassword && (type == .keyDown || type == .flagsChanged) && prompt.observe(event.flags) {
            note?.stringValue = prompt.note
        }
        let pid = event.getIntegerValueField(.eventSourceUnixProcessID)
        let automation = InputPolicy.allow(sourcePID: pid, allowAutomation: allowAutomation, enteringPassword: enteringPassword)
        // Preserve the OS lock shortcut, including its modifier transition, and
        // modifier releases. Do not release our filter on the lock request alone:
        // wait for the system's lock/session notification. Other ordinary key
        // presses remain blocked.
        if filter.allows(type, event, automation: automation) { return Unmanaged.passUnretained(event) }
        // Do not intercept the power key. Forced restart remains an OS recovery path.
        if type.rawValue == 14, let ns = NSEvent(cgEvent: event), ns.subtype.rawValue == 8,
           ((ns.data1 >> 16) & 0xffff) == 6 { return Unmanaged.passUnretained(event) }
        if type == .keyDown {
            let key = event.getIntegerValueField(.keyboardEventKeycode)
            let mods = event.flags.intersection([.maskCommand, .maskControl, .maskAlternate, .maskShift])
            if key == 37 && mods == [.maskCommand, .maskControl, .maskAlternate] {
                DispatchQueue.main.async { [weak self] in self?.showUnlock() }
            } else if enteringPassword && !verifying {
                if key == 53 {
                    auth?.invalidate(); auth = nil
                    prompt = UnlockPrompt(); enteringPassword = false; panel?.orderOut(nil)
                } else if key == 36 || key == 76 {
                    verifyPassword()
                } else if key == 48 {
                    DispatchQueue.main.async { [weak self] in self?.requestTouchID() }
                } else if key == 51 {
                    prompt.deleteLast(); updateDots()
                } else if mods.intersection([.maskCommand, .maskControl, .maskAlternate]).isEmpty {
                    var chars = [UniChar](repeating: 0, count: 8)
                    var count = 0
                    event.keyboardGetUnicodeString(maxStringLength: chars.count, actualStringLength: &count, unicodeString: &chars)
                    prompt.type(String(utf16CodeUnits: chars, count: count)); updateDots()
                    // Typing switches from Touch ID to the passphrase: close the scan request.
                    if auth != nil && !prompt.touchIDPending { auth?.invalidate(); auth = nil; note?.stringValue = prompt.note }
                }
            }
        }
        return nil
    }

    private func checkHealth() {
        guard locked else { return }
        if let deadline = hardDeadline, now >= deadline - 0.25 {
            endLock()
            showSetup()
            message.stringValue = "The 15-second trial finished. Input is enabled again."
            return
        }
        guard recovery?.pulse() == true, AXIsProcessTrusted(), let tap, CFMachPortIsValid(tap), CGEvent.tapIsEnabled(tap: tap), !tapInterrupted else {
            endLock()
            showSetup()
            message.stringValue = "Input Guard stopped because an input or recovery check failed. Your input is NOT locked. Check Accessibility and reopen the app before trying again."
            NSApp.requestUserAttention(.criticalRequest)
            return
        }
        if touchShield.isRaised { touchShield.raise() }
        if !enteringPassword { updatePanelNote() }
        if let deadline = hardDeadline {
            let remaining = max(0, Int(ceil(deadline - now)))
            status.button?.title = " \(testDeadline != nil ? "TEST" : "Agent working") \(remaining / 60):\(String(format: "%02d", remaining % 60))"
        }
    }

    private func showPanel(unlock: Bool) {
        if panel == nil {
            let window = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 460, height: 180),
                                 styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
            window.level = .statusBar
            window.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
            window.isFloatingPanel = true
            window.hidesOnDeactivate = false
            window.isReleasedWhenClosed = false
            window.isRestorable = false
            window.ignoresMouseEvents = true
            window.backgroundColor = .clear
            window.isOpaque = false
            window.contentView?.wantsLayer = true
            window.contentView?.layer?.cornerRadius = 18
            window.contentView?.layer?.masksToBounds = true
            let backdrop = NSVisualEffectView(frame: window.contentView!.bounds)
            backdrop.autoresizingMask = [.width, .height]
            backdrop.material = .popover
            backdrop.state = .active
            window.contentView?.addSubview(backdrop)
            window.hasShadow = true
            let mark = NSImageView(frame: NSRect(x: 24, y: 120, width: 40, height: 40))
            mark.image = Brand.icon(size: 40)
            window.contentView?.addSubview(mark)
            let title = label("", size: 22, weight: .semibold)
            title.frame = NSRect(x: 78, y: 124, width: 358, height: 30)
            let detail = label("")
            detail.frame = NSRect(x: 24, y: 56, width: 412, height: 65)
            let bullets = label("", size: 18, weight: .medium)
            bullets.frame = NSRect(x: 24, y: 20, width: 412, height: 28)
            window.contentView?.addSubview(title)
            window.contentView?.addSubview(detail)
            window.contentView?.addSubview(bullets)
            panelTitle = title; note = detail; dots = bullets; panel = window
        }
        updatePanelNote()
        panel?.center()
        panel?.orderFrontRegardless()
        if !unlock && testDeadline == nil {
            let currentSession = session
            DispatchQueue.main.asyncAfter(deadline: .now() + 4) { [weak self] in
                guard let self, self.session == currentSession, !self.enteringPassword else { return }
                self.panel?.orderOut(nil)
            }
        }
    }

    private func updatePanelNote() {
        if enteringPassword {
            panelTitle?.stringValue = "Resume input"
            note?.stringValue = prompt.note
        } else if let deadline = testDeadline {
            panelTitle?.stringValue = "Input test"
            note?.stringValue = "TEST MODE · releases automatically in \(max(0, Int(ceil(deadline - now)))) seconds.\nTry the keyboard\(touchShield.isRaised ? ", trackpad, and touch screen" : " and trackpad") now."
        } else {
            panelTitle?.stringValue = "Agent working"
            note?.stringValue = "Press ⌃⌥⌘L when you're ready to resume input."
        }
        updateDots()
    }

    private func updateDots() { dots?.stringValue = String(repeating: "•", count: min(prompt.candidate.count, 36)) }

    private func showUnlock() {
        guard locked, !enteringPassword, now - lastPrompt > 1 else { return }
        lastPrompt = now
        enteringPassword = true
        prompt = UnlockPrompt(capsLock: CGEventSource.flagsState(.combinedSessionState).contains(.maskAlphaShift))
        showPanel(unlock: true)
        requestTouchID(automatic: true)
    }

    private func requestTouchID(automatic: Bool = false) {
        guard locked, enteringPassword, record != nil, auth == nil else { return }
        let context = LAContext()
        context.localizedFallbackTitle = ""
        context.localizedCancelTitle = "Use Input Guard Passphrase"
        var error: NSError?
        guard context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &error) else {
            // When the prompt opens, fall back to typing quietly (for example, lid closed).
            guard !automatic else { return }
            prompt.show("Touch ID is unavailable. Type your Input Guard passphrase, then Return.")
            note?.stringValue = prompt.note
            return
        }
        auth = context
        prompt.touchID(pending: true)
        note?.stringValue = prompt.note
        let currentSession = session
        context.evaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, localizedReason: "Unlock Input Guard and restore keyboard and mouse input") { [weak self] success, _ in
            DispatchQueue.main.async {
                guard let self, self.locked, self.session == currentSession, self.auth === context else { return }
                self.auth = nil
                if success { self.endLock() }
                else { self.prompt.touchID(pending: false); self.note?.stringValue = self.prompt.note }
            }
        }
    }

    private func verifyPassword() {
        guard let record, !verifying else { return }
        let submission = prompt.submit(limiter, now: now)
        note?.stringValue = prompt.note; updateDots()
        guard let submitted = submission else { return }
        verifying = true
        let currentSession = session
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let matches = record.matches(submitted)
            DispatchQueue.main.async {
                guard let self, self.locked, self.session == currentSession else { return }
                self.verifying = false
                if matches {
                    let wasTest = self.testDeadline != nil
                    self.endLock()
                    if wasTest {
                        self.showSetup()
                        self.message.stringValue = "Passphrase unlock succeeded. Input is enabled again."
                    }
                }
                else {
                    self.prompt.rejected(&self.limiter, now: self.now)
                    self.note?.stringValue = self.prompt.note
                }
            }
        }
    }

    private func sessionDidReset() {
        // Both lock and unlock clear the guard. Never restore it automatically
        // after login, wake, or a user switch; stale auth replies are discarded
        // by endLock's session UUID reset.
        endLock()
    }

    private func endLock() {
        // Remove the global filter before authentication or UI cleanup, which
        // might itself block. Keep recovery armed until removal is complete.
        locked = false
        if let tap { CGEvent.tapEnable(tap: tap, enable: false); CFMachPortInvalidate(tap) }
        if let source { CFRunLoopRemoveSource(CFRunLoopGetMain(), source, .commonModes) }
        tap = nil; source = nil
        touchShield.lower()
        recovery?.stop(); recovery = nil
        hardDeadline = nil
        session = UUID()
        enteringPassword = false
        verifying = false
        prompt = UnlockPrompt()
        record = nil
        auth?.invalidate(); auth = nil
        watchdog?.invalidate(); watchdog = nil
        testDeadline = nil
        if hasAssertion { IOPMAssertionRelease(assertion); hasAssertion = false }
        if hasDisplayAssertion { IOPMAssertionRelease(displayAssertion); hasDisplayAssertion = false }
        panel?.orderOut(nil)
        updateMenu()
    }
}

@main enum Main {
    static func main() {
        if CommandLine.arguments.contains("--self-test") {
            do { try runSelfTests(); try runTapFilterTests(); try runUnlockPromptTests(); try runSessionResetTests() }
            catch { fputs("\(error.localizedDescription)\n", stderr); exit(1) }
            return
        }
        if CommandLine.arguments.contains("--hotkey-check") {
            _ = NSApplication.shared
            let shortcut = GlobalHotKey { }
            do {
                try shortcut.register()
                shortcut.unregister()
                print("PASS: global shortcut registered and released. No input lock activated.")
            } catch { fputs("\(error.localizedDescription)\n", stderr); exit(1) }
            return
        }
        if CommandLine.arguments.contains("--recovery-tests") {
            do { try runRecoveryTests() } catch { fputs("\(error.localizedDescription)\n", stderr); exit(1) }
            return
        }
        if CommandLine.arguments.contains("--touch-shield-check") {
            NSApplication.shared.setActivationPolicy(.accessory)
            do { try runTouchShieldCheck() } catch { fputs("\(error.localizedDescription)\n", stderr); exit(1) }
            return
        }
        if let index = CommandLine.arguments.firstIndex(of: "--recovery-fixture"), CommandLine.arguments.count > index + 1 {
            runRecoveryFixture(CommandLine.arguments[index + 1])
        }
        let running = NSRunningApplication.runningApplications(withBundleIdentifier: "local.kai.inputguard")
        if let other = running.first(where: { $0.processIdentifier != getpid() }) {
            if CommandLine.arguments.contains("--startup-check") {
                fputs("Close the other Input Guard instance before checking fresh launch.\n", stderr)
                exit(1)
            }
            other.activate(options: [.activateAllWindows]); return
        }
        let app = NSApplication.shared
        // Opt out of macOS Resume, including “Reopen windows when logging back in”.
        // Call once per launch; never pair with enableRelaunchOnLogin().
        app.disableRelaunchOnLogin()
        let delegate = GuardApp()
        app.delegate = delegate
        withExtendedLifetime(delegate) { app.run() }
    }
}
