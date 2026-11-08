import AppKit
import Carbon

final class GlobalHotKey {
    private var reference: EventHotKeyRef?
    private var handler: EventHandlerRef?
    private let action: () -> Void
    private static let signature: OSType = 0x49475244 // IGRD

    init(action: @escaping () -> Void) { self.action = action }

    func register() throws {
        unregister()
        var eventType = EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyPressed))
        let installed = InstallEventHandler(GetApplicationEventTarget(), { _, event, context in
            guard let event, let context else { return OSStatus(eventNotHandledErr) }
            var identifier = EventHotKeyID()
            let result = GetEventParameter(event, EventParamName(kEventParamDirectObject),
                EventParamType(typeEventHotKeyID), nil, MemoryLayout<EventHotKeyID>.size, nil, &identifier)
            guard result == noErr, identifier.signature == GlobalHotKey.signature, identifier.id == 1 else {
                return OSStatus(eventNotHandledErr)
            }
            let owner = Unmanaged<GlobalHotKey>.fromOpaque(context).takeUnretainedValue()
            // Keep modal app work out of Carbon's event callback.
            DispatchQueue.main.async { [weak owner] in owner?.action() }
            return noErr
        }, 1, &eventType, Unmanaged.passUnretained(self).toOpaque(), &handler)
        guard installed == noErr else {
            throw GuardError.message("Could not enable the keyboard shortcut (\(installed)). Use the menu-bar padlock.")
        }
        let registered = RegisterEventHotKey(UInt32(kVK_ANSI_L), UInt32(controlKey | optionKey | cmdKey),
            EventHotKeyID(signature: Self.signature, id: 1), GetApplicationEventTarget(),
            UInt32(kEventHotKeyExclusive), &reference)
        guard registered == noErr else {
            unregister()
            throw GuardError.message("Control–Option–Command–L is unavailable, possibly because another app uses it (\(registered)). Use the menu-bar padlock.")
        }
    }

    func unregister() {
        if let reference { UnregisterEventHotKey(reference) }
        if let handler { RemoveEventHandler(handler) }
        reference = nil; handler = nil
    }

    deinit { unregister() }
}
