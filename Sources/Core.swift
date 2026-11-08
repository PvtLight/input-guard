import Foundation
import Security
import CommonCrypto
import CoreGraphics

enum GuardError: LocalizedError {
    case message(String)
    var errorDescription: String? { if case .message(let text) = self { return text }; return nil }
}

struct PasswordRecord: Codable {
    let version: Int
    let salt: Data
    let digest: Data
    let rounds: UInt32

    static func derive(_ password: String, salt: Data, rounds: UInt32) throws -> Data {
        let bytes = Array(password.utf8)
        var output = [UInt8](repeating: 0, count: 32)
        let status = bytes.withUnsafeBytes { passwordBuffer in
            salt.withUnsafeBytes { saltBuffer in
                CCKeyDerivationPBKDF(CCPBKDFAlgorithm(kCCPBKDF2),
                    passwordBuffer.baseAddress!.assumingMemoryBound(to: Int8.self), bytes.count,
                    saltBuffer.baseAddress!.assumingMemoryBound(to: UInt8.self), salt.count,
                    CCPseudoRandomAlgorithm(kCCPRFHmacAlgSHA256), rounds, &output, output.count)
            }
        }
        guard status == kCCSuccess else { throw GuardError.message("Could not prepare the passphrase.") }
        return Data(output)
    }

    static func create(_ password: String) throws -> PasswordRecord {
        guard validPassword(password) else {
            throw GuardError.message("Use 4–128 characters: letters, numbers, spaces, or punctuation from an English keyboard. Use a separate passphrase, not your Mac password.")
        }
        var bytes = [UInt8](repeating: 0, count: 32)
        guard SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes) == errSecSuccess else {
            throw GuardError.message("Could not generate a secure salt.")
        }
        let salt = Data(bytes)
        return PasswordRecord(version: 1, salt: salt,
                              digest: try derive(password, salt: salt, rounds: 600_000), rounds: 600_000)
    }

    static func validPassword(_ password: String) -> Bool {
        (4...128).contains(password.utf8.count) && password.utf8.allSatisfy { (32...126).contains($0) }
    }

    func validate() throws {
        guard version == 1, salt.count == 32, digest.count == 32, rounds == 600_000 else {
            throw GuardError.message("The saved passphrase record is invalid. Input Guard will not lock.")
        }
    }

    func matches(_ candidate: String) -> Bool {
        guard (try? validate()) != nil, !candidate.isEmpty, candidate.utf8.count <= 128,
              let actual = try? Self.derive(candidate, salt: salt, rounds: rounds) else { return false }
        return zip(actual, digest).reduce(UInt8(0)) { $0 | ($1.0 ^ $1.1) } == 0
    }
}

enum Vault {
    static var query: [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: "local.kai.inputguard.passphrase",
         kSecAttrAccount as String: "owner"]
    }

    static func read() throws -> PasswordRecord? {
        var request = query
        request[kSecReturnData as String] = true
        request[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(request as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess, let data = result as? Data else {
            throw GuardError.message("Keychain access failed (\(status)). Input Guard will not lock.")
        }
        let record = try JSONDecoder().decode(PasswordRecord.self, from: data)
        try record.validate()
        return record
    }

    static func save(_ record: PasswordRecord) throws {
        let data = try JSONEncoder().encode(record)
        let attributes = [kSecValueData as String: data]
        var status = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            var item = query
            item[kSecValueData as String] = data
            item[kSecAttrLabel as String] = "Input Guard passphrase verifier"
            status = SecItemAdd(item as CFDictionary, nil)
        }
        guard status == errSecSuccess else { throw GuardError.message("Could not save to Keychain (\(status)).") }
    }
}

struct AttemptLimiter {
    private(set) var failures = 0
    private(set) var nextAttempt: TimeInterval = 0
    mutating func failed(at now: TimeInterval) {
        failures += 1
        nextAttempt = now + min(30, pow(2, Double(min(failures, 5))))
    }
    func canTry(at now: TimeInterval) -> Bool { now >= nextAttempt }
}

// The unlock prompt's typed passphrase and message, shared by the app and the
// self-tests. Return with nothing typed is not a guess, Return during a wait
// keeps what was typed, and the prompt says when Caps Lock is on. Touch ID
// starts when the prompt opens; typing switches to the passphrase.
struct UnlockPrompt {
    static let instructions = "Type your passphrase, then Return. Tab starts Touch ID.\nEscape returns to Agent working."
    static let touchIDReady = "Touch ID is ready. Or type your passphrase, then Return.\nEscape returns to Agent working."
    private(set) var candidate = ""
    private(set) var message = instructions
    private(set) var capsLock: Bool
    private(set) var touchIDPending = false

    init(capsLock: Bool = false) { self.capsLock = capsLock }

    var note: String { capsLock ? message + "\nCaps Lock is on." : message }

    // True when Caps Lock changed, so the note needs redrawing.
    mutating func observe(_ flags: CGEventFlags) -> Bool {
        let on = flags.contains(.maskAlphaShift)
        guard on != capsLock else { return false }
        capsLock = on
        return true
    }

    // Printable ASCII only, up to 128 characters, as saved passphrases are.
    mutating func type(_ text: String) {
        guard !text.isEmpty && text.utf8.allSatisfy({ (32...126).contains($0) }) && candidate.utf8.count + text.utf8.count <= 128 else { return }
        candidate += text
        if touchIDPending { touchID(pending: false) }
    }

    // Touch ID started, or ended without unlocking (failed or canceled).
    mutating func touchID(pending: Bool) {
        touchIDPending = pending
        message = pending ? Self.touchIDReady : Self.instructions
    }

    mutating func deleteLast() { if !candidate.isEmpty { candidate.removeLast() } }

    mutating func show(_ text: String) { message = text }

    // The passphrase to check now, or nil when there is nothing to check.
    mutating func submit(_ limiter: AttemptLimiter, now: TimeInterval) -> String? {
        guard !candidate.isEmpty else { return nil }
        guard limiter.canTry(at: now) else {
            message = "Too soon. What you typed is kept. Press Return again in \(Self.seconds(limiter.nextAttempt - now))."
            return nil
        }
        let submitted = candidate
        candidate = ""
        message = "Checking passphrase…"
        return submitted
    }

    // A passphrase that did not match counts as a failed attempt.
    mutating func rejected(_ limiter: inout AttemptLimiter, now: TimeInterval) {
        limiter.failed(at: now)
        message = "That passphrase did not match. Wait \(Self.seconds(limiter.nextAttempt - now)), then try again."
    }

    static func seconds(_ interval: TimeInterval) -> String {
        let count = max(1, Int(ceil(interval)))
        return count == 1 ? "1 second" : "\(count) seconds"
    }
}

enum InputPolicy {
    // Provenance is a compatibility heuristic, never an authentication boundary.
    // Processes can synthesize events; remapping tools can also relabel real input.
    static func allow(sourcePID: Int64, allowAutomation: Bool, enteringPassword: Bool) -> Bool {
        allowAutomation && !enteringPassword && sourcePID > 0
    }
}

// Which events the guard's tap lets through. The live tap and the self-tests
// use these same rules. Besides automation (when allowed), only the macOS lock
// shortcut Control–Command–Q passes, with its modifier transition, plus
// releases of modifiers that other apps have already seen pressed. Without
// those releases, apps keep believing a modifier is held after unlock (for
// example the Control–Option–Command of the shortcut) until it is pressed again.
struct TapFilter {
    static let modifiers: CGEventFlags = [.maskCommand, .maskControl, .maskAlternate, .maskShift, .maskSecondaryFn]
    private(set) var seenByApps: CGEventFlags

    init(held: CGEventFlags) { seenByApps = held.intersection(Self.modifiers) }

    mutating func allows(_ type: CGEventType, _ event: CGEvent, automation: Bool) -> Bool {
        let active = event.flags.intersection([.maskCommand, .maskControl, .maskAlternate, .maskShift])
        let systemLock = active == [.maskCommand, .maskControl]
        guard type == .flagsChanged else {
            return automation || (systemLock && (type == .keyDown || type == .keyUp) &&
                event.getIntegerValueField(.keyboardEventKeycode) == 12)
        }
        let held = event.flags.intersection(Self.modifiers)
        // Never pass a new press, except the lock shortcut's: only releases
        // that bring other apps back toward the keys really held.
        guard automation || systemLock || (held != seenByApps && seenByApps.isSuperset(of: held)) else { return false }
        seenByApps = held
        return true
    }
}

func runSelfTests() throws {
    func check(_ ok: @autoclosure () -> Bool, _ name: String) throws {
        if !ok() { throw GuardError.message("FAILED: \(name)") }
        print("PASS: \(name)")
    }
    let vector = try PasswordRecord.derive("password", salt: Data("salt".utf8), rounds: 1)
    try check(vector.map { String(format: "%02x", $0) }.joined() ==
              "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b", "PBKDF2-SHA256 known answer")
    let record = try PasswordRecord.create("a long test passphrase!")
    try check(record.matches("a long test passphrase!"), "Correct passphrase unlocks")
    try check(!record.matches("a long wrong passphrase!"), "Wrong passphrase stays locked")
    try check(!record.matches(""), "Empty passphrase stays locked")
    try check(!PasswordRecord.validPassword("123"), "Reject fewer than four characters")
    let shortRecord = try PasswordRecord.create("2468")
    try check(shortRecord.matches("2468"), "Four-character passphrase can be saved and verified")
    try check(!shortRecord.matches("2469"), "Wrong four-character passphrase stays locked")
    try check(!PasswordRecord.validPassword("漢字 passphrase long"), "Reject unsupported input composition")
    let corrupt = PasswordRecord(version: 1, salt: Data(), digest: Data(), rounds: 1)
    try check(!corrupt.matches("anything"), "Corrupt record fails closed")
    var limiter = AttemptLimiter()
    limiter.failed(at: 10)
    try check(!limiter.canTry(at: 11) && limiter.canTry(at: 12), "Failed authentication backs off")
    for _ in 0..<20 { limiter.failed(at: 100) }
    try check(limiter.nextAttempt == 130, "Backoff caps at 30 seconds")
    try check(!InputPolicy.allow(sourcePID: 0, allowAutomation: true, enteringPassword: false), "Physical events blocked in automation mode")
    try check(InputPolicy.allow(sourcePID: 123, allowAutomation: true, enteringPassword: false), "Tagged process events pass in automation mode")
    try check(!InputPolicy.allow(sourcePID: 123, allowAutomation: false, enteringPassword: false), "Strict mode blocks synthetic events")
    try check(!InputPolicy.allow(sourcePID: 123, allowAutomation: true, enteringPassword: true), "Password entry suspends synthetic events")
    print("All self-tests passed. No input hooks, Keychain changes, or permissions used.")
}

// Replays physical key sequences through TapFilter. Other apps track held
// modifiers from the flagsChanged events they receive; after every sequence
// they must agree with the keys really held. No tap is created.
func runTapFilterTests() throws {
    func check(_ ok: @autoclosure () -> Bool, _ name: String) throws {
        if !ok() { throw GuardError.message("FAILED: \(name)") }
        print("PASS: \(name)")
    }
    // Hardware-like flags: generic mask plus left-side device bit, as macOS sends.
    let device: [CGKeyCode: (CGEventFlags, UInt64)] = [59: (.maskControl, 0x1), 56: (.maskShift, 0x2),
        55: (.maskCommand, 0x8), 58: (.maskAlternate, 0x20), 63: (.maskSecondaryFn, 0)]
    struct Replay {
        let device: [CGKeyCode: (CGEventFlags, UInt64)]
        var held: [CGKeyCode] = []
        var filter: TapFilter?
        var apps: CGEventFlags = []
        var pressesLeaked = 0
        var keysLeaked: [CGKeyCode] = []
        var flags: CGEventFlags {
            var raw: UInt64 = 0x100
            for code in held { raw |= device[code]!.0.rawValue | device[code]!.1 }
            return CGEventFlags(rawValue: raw)
        }
        @discardableResult mutating func send(_ type: CGEventType, _ code: CGKeyCode, automation: Bool = false) -> Bool {
            let event = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: type != .keyUp)!
            event.type = type
            event.flags = flags
            let guarded = filter != nil
            guard filter?.allows(type, event, automation: automation) ?? true else { return false }
            if type == .flagsChanged {
                let next = flags.intersection(TapFilter.modifiers)
                let lockPair = next.intersection([.maskCommand, .maskControl, .maskAlternate, .maskShift]) == [.maskCommand, .maskControl]
                if guarded && !automation && !apps.isSuperset(of: next) && !lockPair { pressesLeaked += 1 }
                apps = next
            } else if guarded && !automation { keysLeaked.append(code) }
            return true
        }
        mutating func modifier(_ code: CGKeyCode, down: Bool, automation: Bool = false) {
            if down { held.append(code) } else { held.removeAll { $0 == code } }
            send(.flagsChanged, code, automation: automation)
        }
        mutating func type(_ code: CGKeyCode) { send(.keyDown, code); send(.keyUp, code) }
        mutating func lock() { filter = TapFilter(held: flags) }
        mutating func unlock() { filter = nil }
    }
    func orders(_ keys: [CGKeyCode]) -> [[CGKeyCode]] {
        keys.count <= 1 ? [keys] : keys.indices.flatMap { i -> [[CGKeyCode]] in
            var rest = keys; let first = rest.remove(at: i)
            return orders(rest).map { [first] + $0 }
        }
    }
    let chord = orders([59, 58, 55]) // Control, Option, Command in every order
    var runs = 0, stuck = 0, presses = 0, keys = 0
    for pressLock in chord { for releaseLock in chord { for tapAfter in 0...3 {
        for pressUnlock in chord { for releaseUnlock in chord {
            var r = Replay(device: device)
            pressLock.forEach { r.modifier($0, down: true) }
            // L is taken by the hotkey; the tap may start before the chord is released.
            for (index, code) in releaseLock.enumerated() {
                if index == tapAfter { r.lock() }
                r.modifier(code, down: false)
            }
            if tapAfter == 3 { r.lock() }
            pressUnlock.forEach { r.modifier($0, down: true) }
            r.type(37)
            releaseUnlock.forEach { r.modifier($0, down: false) }
            r.modifier(56, down: true); r.type(31); r.modifier(56, down: false) // Shift-O
            for code: CGKeyCode in [40, 49, 5, 31] { r.type(code) } // "k go"
            r.send(.keyDown, 36)
            r.unlock() // passphrase verified
            r.send(.keyUp, 36)
            runs += 1
            if r.apps != [] { stuck += 1 }
            presses += r.pressesLeaked; keys += r.keysLeaked.count
        } }
    } } }
    try check(stuck == 0, "\(runs - stuck) of \(runs) lock/unlock key orders leave no modifier held in other apps")
    try check(presses == 0 && keys == 0, "No key or modifier press reaches other apps during \(runs) lock/unlock sequences")

    var lockShortcuts = 0
    for press in orders([59, 55]) { for release in orders([59, 55]) { for endAfter in 0...3 {
        var r = Replay(device: device)
        r.lock()
        press.forEach { r.modifier($0, down: true) }
        let down = r.send(.keyDown, 12)
        if endAfter == 0 { r.unlock() } // macOS lock notification
        let up = r.send(.keyUp, 12)
        for (index, code) in release.enumerated() {
            if endAfter == index + 1 { r.unlock() }
            r.modifier(code, down: false)
        }
        if down && up && r.apps == [] && r.pressesLeaked == 0 { lockShortcuts += 1 }
    } } }
    try check(lockShortcuts == 16, "Control–Command–Q passes in \(lockShortcuts) of 16 orders and leaves no modifier held")

    var quiet = Replay(device: device)
    quiet.lock()
    quiet.modifier(56, down: true); quiet.type(31); quiet.modifier(56, down: false)
    quiet.modifier(58, down: true); quiet.type(31); quiet.modifier(58, down: false)
    quiet.modifier(63, down: true); quiet.modifier(63, down: false)
    try check(quiet.apps == [] && quiet.pressesLeaked == 0 && quiet.keysLeaked.isEmpty,
              "Typing with Shift, Option, or Fn sends nothing to other apps while guarded")
    var blocked = 0
    let shortcuts: [([CGKeyCode], CGKeyCode)] = [([59, 55], 13), ([59, 58, 55], 12), ([56, 59, 55], 12),
        ([55], 12), ([59], 12), ([], 31), ([59, 58, 55], 31), ([55], 31), ([59, 55], 31)]
    for (mods, key) in shortcuts {
        var r = Replay(device: device)
        r.lock()
        mods.forEach { r.modifier($0, down: true) }
        r.type(key)
        mods.reversed().forEach { r.modifier($0, down: false) }
        if r.keysLeaked.isEmpty { blocked += 1 }
    }
    try check(blocked == shortcuts.count, "Other shortcuts and plain keys stay blocked (\(blocked) of \(shortcuts.count))")
    var automated = Replay(device: device)
    automated.lock()
    automated.modifier(55, down: true, automation: true)
    let automatedKey = automated.send(.keyDown, 31, automation: true)
    automated.modifier(55, down: false, automation: true)
    try check(automatedKey && automated.apps == [] && !automated.send(.keyDown, 31),
              "Allowed automation still passes; physical keys stay blocked")

    // Seeded random sessions: presses never leak, and once every key is up,
    // other apps agree that nothing is held.
    var seed: UInt64 = 0x1D6A_2E55
    func next(_ bound: Int) -> Int {
        seed = seed &* 6364136223846793005 &+ 1442695040888963407
        return Int((seed >> 33) % UInt64(bound))
    }
    var fuzz = Replay(device: device)
    fuzz.lock()
    var events = 0, outOfSync = 0
    let pool: [CGKeyCode] = [59, 58, 55, 56, 63]
    for _ in 0..<50_000 {
        switch next(10) {
        case 0...5:
            let code = pool[next(pool.count)]
            fuzz.modifier(code, down: !fuzz.held.contains(code))
        case 6...8: fuzz.type([31, 12, 37, 13, 36][next(5)])
        default: if next(20) == 0 { fuzz.filter == nil ? fuzz.lock() : fuzz.unlock() }
        }
        events += 1
        if fuzz.held.isEmpty && fuzz.apps != [] { outOfSync += 1 }
    }
    try check(outOfSync == 0, "\(events) random key events: other apps never keep a modifier after all keys are up")
    try check(fuzz.pressesLeaked == 0 && fuzz.keysLeaked.allSatisfy { $0 == 12 },
              "Random key events: only Control–Command–Q and modifier releases pass while guarded")
}

// Replays the unlock prompt with a real passphrase record and attempt limiter,
// the way the app answers each Return. No tap is created.
func runUnlockPromptTests() throws {
    func check(_ ok: @autoclosure () -> Bool, _ name: String) throws {
        if !ok() { throw GuardError.message("FAILED: \(name)") }
        print("PASS: \(name)")
    }
    let record = try PasswordRecord.create("Open sesame 42")
    // True when the passphrase matched. A mismatch counts, as in the app.
    func pressReturn(_ prompt: inout UnlockPrompt, _ limiter: inout AttemptLimiter, at now: TimeInterval) -> Bool {
        guard let submitted = prompt.submit(limiter, now: now) else { return false }
        if record.matches(submitted) { return true }
        prompt.rejected(&limiter, now: now)
        return false
    }
    func capsEvent(_ type: CGEventType, _ code: CGKeyCode, on: Bool) -> CGEvent {
        let event = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: true)!
        event.type = type
        event.flags = CGEventFlags(rawValue: 0x100 | (on ? CGEventFlags.maskAlphaShift.rawValue : 0))
        return event
    }

    var limiter = AttemptLimiter()
    var prompt = UnlockPrompt()
    let unlocked = pressReturn(&prompt, &limiter, at: 10)
    try check(!unlocked && limiter.failures == 0 && limiter.canTry(at: 10) && prompt.message == UnlockPrompt.instructions,
              "Return with nothing typed is not checked, counted, or made to wait")

    limiter = AttemptLimiter(); prompt = UnlockPrompt()
    prompt.type("wrong guess")
    _ = pressReturn(&prompt, &limiter, at: 10) // must wait until 12
    prompt.type("Open sesame 42")
    let early = pressReturn(&prompt, &limiter, at: 11)
    let kept = prompt.candidate == "Open sesame 42" && prompt.message.hasSuffix("Press Return again in 1 second.")
    let later = pressReturn(&prompt, &limiter, at: 12)
    try check(!early && kept && later && limiter.failures == 1,
              "Return during a wait keeps what you typed; Return again after the wait unlocks")

    prompt = UnlockPrompt()
    let turnedOn = prompt.observe(capsEvent(.flagsChanged, 57, on: true).flags)
    let shown = prompt.note == UnlockPrompt.instructions + "\nCaps Lock is on."
    prompt.type("OPEN"); _ = pressReturn(&prompt, &limiter, at: 20)
    let afterMismatch = prompt.note.hasPrefix("That passphrase did not match.") && prompt.note.hasSuffix("\nCaps Lock is on.")
    let typingOn = !prompt.observe(capsEvent(.keyDown, 31, on: true).flags)
    let turnedOff = prompt.observe(capsEvent(.flagsChanged, 57, on: false).flags)
    try check(turnedOn && shown && afterMismatch && typingOn && turnedOff && !prompt.note.contains("Caps Lock") &&
              UnlockPrompt(capsLock: true).note.hasSuffix("\nCaps Lock is on."),
              "Caps Lock is pointed out while on, also after a mismatch, and cleared when off")

    limiter = AttemptLimiter(); prompt = UnlockPrompt()
    prompt.type("not it")
    let rejected = !pressReturn(&prompt, &limiter, at: 50)
    try check(rejected && limiter.failures == 1 && !limiter.canTry(at: 51) && prompt.candidate.isEmpty &&
              prompt.message == "That passphrase did not match. Wait 2 seconds, then try again.",
              "A wrong passphrase still counts, clears, and starts a 2-second wait")
    limiter = AttemptLimiter(); prompt = UnlockPrompt()
    prompt.type("Open sesame 42")
    try check(pressReturn(&prompt, &limiter, at: 0) && limiter.failures == 0 && prompt.candidate.isEmpty,
              "The right passphrase unlocks on the first try")
    var touch = UnlockPrompt()
    touch.touchID(pending: true) // as when the prompt opens
    let ready = touch.note == UnlockPrompt.touchIDReady
    touch.type("ø") // a key that types nothing keeps Touch ID waiting
    let keptWaiting = touch.touchIDPending
    touch.type("a")
    let switched = !touch.touchIDPending && touch.message == UnlockPrompt.instructions && touch.candidate == "a"
    touch.touchID(pending: true); touch.touchID(pending: false) // Tab, then a failed scan
    try check(ready && keptWaiting && switched && touch.message == UnlockPrompt.instructions && touch.candidate == "a",
              "Touch ID shows as ready; typing switches to the passphrase; a failed scan returns to typing")
    var typing = UnlockPrompt()
    for text in ["ab", "é", "\u{7f}", "", "ø", " C~"] { typing.type(text) }
    typing.deleteLast()
    var long = UnlockPrompt()
    for _ in 0..<130 { long.type("x") }
    try check(typing.candidate == "ab C" && long.candidate.utf8.count == 128,
              "Typing still takes printable ASCII only, up to 128 characters, and Delete removes one")
}
