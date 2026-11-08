import Foundation
import Darwin

final class RecoverySupervisor {
    private var child: Process?
    private var commands: FileHandle?
    private var acknowledgements: FileHandle?
    private var lastAck: TimeInterval = 0
    private(set) var deadline: TimeInterval?

    var childPID: pid_t? { child?.processIdentifier }

    func start(seconds: TimeInterval?, executable: URL? = nil) throws {
        guard child == nil, seconds.map({ $0.isFinite && (1...RecoveryClock.maximumTimedSession).contains($0) }) ?? true else {
            throw GuardError.message("Invalid recovery session. Input blocking was not started.")
        }
        let process = Process()
        process.executableURL = executable ?? URL(fileURLWithPath: CommandLine.arguments[0])
            .deletingLastPathComponent().appendingPathComponent("InputGuardRecovery")
        process.arguments = [seconds.map { String($0) } ?? "until-unlock"]
        let toChild = Pipe()
        let fromChild = Pipe()
        process.standardInput = toChild
        process.standardOutput = fromChild
        process.standardError = FileHandle.nullDevice
        deadline = seconds.map { RecoveryClock.now + $0 }
        try process.run()
        child = process
        toChild.fileHandleForReading.closeFile()
        fromChild.fileHandleForWriting.closeFile()
        commands = toChild.fileHandleForWriting
        acknowledgements = fromChild.fileHandleForReading
        _ = fcntl(commands!.fileDescriptor, F_SETFL, O_NONBLOCK)
        _ = fcntl(commands!.fileDescriptor, F_SETNOSIGPIPE, 1)
        _ = fcntl(acknowledgements!.fileDescriptor, F_SETFL, O_NONBLOCK)
        let startupDeadline = RecoveryClock.now + 1.5
        while RecoveryClock.now < startupDeadline {
            var response: UInt8 = 0
            let count = read(acknowledgements!.fileDescriptor, &response, 1)
            if count == 1 && response == 82 {
                lastAck = RecoveryClock.heartbeatNow
                return
            }
            if count == 0 || !process.isRunning || (count == 1 && response != 82) { break }
            var item = pollfd(fd: acknowledgements!.fileDescriptor, events: Int16(POLLIN), revents: 0)
            _ = poll(&item, 1, 25)
        }
        stop()
        throw GuardError.message("The independent recovery process did not start. Input blocking was not started.")
    }

    // Called only by the UI event loop. Moving this to a background queue would
    // hide a frozen UI from the independent recovery process.
    func pulse() -> Bool {
        guard let child, child.isRunning, let commands, let acknowledgements,
              deadline.map({ RecoveryClock.now < $0 }) ?? true else { return false }
        var response = [UInt8](repeating: 0, count: 256)
        let count = read(acknowledgements.fileDescriptor, &response, response.count)
        if count > 0 {
            guard response.prefix(count).allSatisfy({ $0 == 65 }) else { return false }
            lastAck = RecoveryClock.heartbeatNow
        } else if count == 0 || (errno != EAGAIN && errno != EINTR) { return false }
        guard RecoveryClock.heartbeatNow - lastAck < RecoveryClock.acknowledgementTimeout else { return false }
        var heartbeat: UInt8 = 72
        return write(commands.fileDescriptor, &heartbeat, 1) == 1
    }

    // Must be called AFTER the event tap has been removed.
    func stop() {
        if let commands {
            var disarm: UInt8 = 68
            _ = write(commands.fileDescriptor, &disarm, 1)
            commands.closeFile()
        }
        // Keep the read end alive until the child sees D, so an in-flight ACK
        // cannot fail with EPIPE and accidentally trigger a rescue during teardown.
        if let child, let acknowledgements {
            // If the helper was suspended, let it consume the disarm message.
            // The D command takes priority over expired timers in the helper.
            if child.isRunning { _ = kill(child.processIdentifier, SIGCONT) }
            DispatchQueue.global().async {
                child.waitUntilExit()
                acknowledgements.closeFile()
            }
        } else { acknowledgements?.closeFile() }
        commands = nil
        acknowledgements = nil
        child = nil
        deadline = nil
    }
}

// These fixtures never create an NSApplication or an event tap. The helper is
// allowed to terminate ONLY the disposable fixture process that launched it.
func runRecoveryFixture(_ mode: String) -> Never {
    let supervisor = RecoverySupervisor()
    do {
        if mode == "missing-helper" {
            do {
                try supervisor.start(seconds: 3, executable: URL(fileURLWithPath: "/no-such-input-guard-helper"))
                exit(10)
            } catch { exit(0) }
        }
        let duration: TimeInterval? = (mode == "freeze" || mode == "indefinite-healthy") ? nil : (mode == "expiry" ? 2 : 15)
        try supervisor.start(seconds: duration)
        if mode == "indefinite-healthy", supervisor.deadline != nil { exit(16) }
        if mode == "freeze" { raise(SIGSTOP); exit(11) }
        if mode == "helper-exit", let pid = supervisor.childPID { kill(pid, SIGKILL) }
        if mode == "helper-freeze", let pid = supervisor.childPID { kill(pid, SIGSTOP) }
        let start = RecoveryClock.now
        while RecoveryClock.now - start < 6 {
            if !supervisor.pulse() {
                // The real app removes its tap before stopping the helper here.
                supervisor.stop()
                if mode == "helper-exit" || mode == "helper-freeze" { exit(0) }
                if mode != "expiry" { exit(12) }
            }
            if mode == "clean-stop" && RecoveryClock.now - start > 0.5 {
                supervisor.stop()
                Thread.sleep(forTimeInterval: 3.5)
                exit(0)
            }
            if mode == "indefinite-healthy" && RecoveryClock.now - start > 4 {
                supervisor.stop()
                exit(0)
            }
            // For expiry, ignore the app-side deadline to exercise the helper's
            // own hard stop even while its parent remains responsive.
            if mode == "expiry" && RecoveryClock.now - start > 1.5 {
                Thread.sleep(forTimeInterval: 5)
                exit(13)
            }
            Thread.sleep(forTimeInterval: 0.1)
        }
        supervisor.stop()
        exit(14)
    } catch {
        fputs("Recovery fixture failed: \(error)\n", stderr)
        exit(15)
    }
}

func runRecoveryTests() throws {
    for (mode, killed) in [("freeze", true), ("expiry", true), ("helper-exit", false),
                            ("helper-freeze", false), ("clean-stop", false), ("missing-helper", false),
                            ("indefinite-healthy", false)] {
        let fixture = Process()
        fixture.executableURL = URL(fileURLWithPath: CommandLine.arguments[0])
        fixture.arguments = ["--recovery-fixture", mode]
        fixture.standardOutput = FileHandle.nullDevice
        try fixture.run()
        let deadline = RecoveryClock.now + 9
        while fixture.isRunning && RecoveryClock.now < deadline { Thread.sleep(forTimeInterval: 0.05) }
        if fixture.isRunning { kill(fixture.processIdentifier, SIGKILL); fixture.waitUntilExit(); throw GuardError.message("Recovery test timed out: \(mode)") }
        fixture.waitUntilExit()
        guard killed ? (fixture.terminationReason == .uncaughtSignal && fixture.terminationStatus == SIGKILL) :
                       (fixture.terminationReason == .exit && fixture.terminationStatus == 0) else {
            throw GuardError.message("Recovery test failed: \(mode) (\(fixture.terminationStatus))")
        }
        print("PASS: recovery \(mode)")
    }
    print("Recovery integration tests passed. No physical input was blocked.")
}
