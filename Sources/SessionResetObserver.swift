import AppKit

// A safety reset, not authentication. The screen-lock names are system
// distributed notifications, not a documented/stable AppKit lock-state API.
// Keep the independently tested recovery mechanism as a separate safeguard.
final class SessionResetObserver: NSObject {
    static let screenEvents = ["com.apple.screenIsLocked", "com.apple.screenIsUnlocked"].map { Notification.Name($0) }
    static let workspaceEvents: [Notification.Name] = [
        NSWorkspace.sessionDidResignActiveNotification, NSWorkspace.sessionDidBecomeActiveNotification
    ]
    private let workspace: NotificationCenter
    private let distributed: NotificationCenter
    private let onReset: () -> Void

    init(workspace: NotificationCenter = NSWorkspace.shared.notificationCenter,
         distributed: NotificationCenter = DistributedNotificationCenter.default(),
         onReset: @escaping () -> Void) {
        self.workspace = workspace
        self.distributed = distributed
        self.onReset = onReset
        super.init()
        for name in Self.workspaceEvents {
            workspace.addObserver(self, selector: #selector(received), name: name, object: nil)
        }
        for name in Self.screenEvents {
            if let center = distributed as? DistributedNotificationCenter {
                center.addObserver(self, selector: #selector(received), name: name, object: nil,
                                   suspensionBehavior: .deliverImmediately)
            } else {
                distributed.addObserver(self, selector: #selector(received), name: name, object: nil)
            }
        }
    }

    @objc private func received(_ notification: Notification) {
        if Thread.isMainThread { onReset() }
        else { DispatchQueue.main.async { [weak self] in self?.onReset() } }
    }

    deinit {
        workspace.removeObserver(self)
        distributed.removeObserver(self)
    }
}

func runSessionResetTests() throws {
    // Private, in-process centers: never post fake system lock notifications.
    let workspace = NotificationCenter()
    let distributed = NotificationCenter()
    var resets = 0
    var observer: SessionResetObserver? = SessionResetObserver(workspace: workspace, distributed: distributed) { resets += 1 }
    guard observer != nil else { throw GuardError.message("Session observer missing") }
    for name in SessionResetObserver.screenEvents {
        let previous = resets
        distributed.post(name: name, object: nil)
        guard resets == previous + 1 else { throw GuardError.message("Screen reset notification was missed") }
    }
    print("PASS: screen lock and unlock both request a safety reset")
    for name in SessionResetObserver.workspaceEvents {
        let previous = resets
        workspace.post(name: name, object: nil)
        guard resets == previous + 1 else { throw GuardError.message("Session reset notification was missed") }
    }
    print("PASS: switching away from and back to the user session requests a safety reset")
    let previous = resets
    distributed.post(name: Notification.Name("unrelated"), object: nil)
    workspace.post(name: NSWorkspace.screensDidSleepNotification, object: nil)
    workspace.post(name: NSWorkspace.didWakeNotification, object: nil)
    guard resets == previous else { throw GuardError.message("Unrelated notification reset the session") }
    print("PASS: unrelated notifications and display sleep/wake do not unlock input")
    observer = nil
    distributed.post(name: SessionResetObserver.screenEvents[0], object: nil)
    guard resets == previous else { throw GuardError.message("Session observer failed to detach") }
    print("PASS: session observer detaches cleanly")
}
