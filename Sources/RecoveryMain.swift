import Foundation
import Darwin

// Separate executable, separate event loop. No Accessibility, key events, UI,
// networking, PID arguments, or administrator privileges.
@main enum RecoveryMain {
    static func main() {
        guard CommandLine.arguments.count == 2 else { exit(64) }
        let duration: TimeInterval?
        if CommandLine.arguments[1] == "until-unlock" { duration = nil }
        else {
            guard let value = Double(CommandLine.arguments[1]), value.isFinite,
                  (1...RecoveryClock.maximumTimedSession).contains(value) else { exit(64) }
            duration = value
        }
        let parent = getppid()
        guard parent > 1 else { exit(65) }
        let deadline = duration.map { RecoveryClock.now + $0 }
        var lastHeartbeat = RecoveryClock.heartbeatNow
        _ = fcntl(STDIN_FILENO, F_SETFL, O_NONBLOCK)
        _ = fcntl(STDOUT_FILENO, F_SETFL, O_NONBLOCK)
        _ = fcntl(STDOUT_FILENO, F_SETNOSIGPIPE, 1)
        var ready: UInt8 = 82 // R
        guard write(STDOUT_FILENO, &ready, 1) == 1 else { exit(66) }
        while true {
            // Reparenting prevents signalling an unrelated process after PID reuse.
            guard getppid() == parent else { exit(0) }
            var bytes = [UInt8](repeating: 0, count: 256)
            let count = read(STDIN_FILENO, &bytes, bytes.count)
            if count > 0 {
                let received = bytes.prefix(count)
                // Disarm is sent only after the application removes its input tap.
                if received.contains(68) { exit(0) } // D
                guard received.allSatisfy({ $0 == 72 }) else { rescue(parent) } // H
                lastHeartbeat = RecoveryClock.heartbeatNow
                var acknowledgement: UInt8 = 65 // A
                if write(STDOUT_FILENO, &acknowledgement, 1) != 1 { rescue(parent) }
            } else if count == 0 || (errno != EAGAIN && errno != EINTR) {
                rescue(parent)
            }
            if (deadline.map { RecoveryClock.now >= $0 } ?? false) || RecoveryClock.heartbeatNow - lastHeartbeat >= RecoveryClock.heartbeatTimeout {
                rescue(parent)
            }
            var item = pollfd(fd: STDIN_FILENO, events: Int16(POLLIN), revents: 0)
            _ = poll(&item, 1, 50)
        }
    }

    private static func rescue(_ parent: pid_t) -> Never {
        if parent > 1 && getppid() == parent { _ = kill(parent, SIGKILL) }
        exit(0)
    }
}
