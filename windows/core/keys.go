package core

// Windows virtual-key codes the guard cares about. Mouse buttons share the
// same numbering (VK_LBUTTON and so on), so one table tracks both.
const (
	VKLButton  = 0x01
	VKRButton  = 0x02
	VKMButton  = 0x04
	VKXButton1 = 0x05
	VKXButton2 = 0x06
	VKBack     = 0x08
	VKTab      = 0x09
	VKReturn   = 0x0D
	VKShift    = 0x10
	VKControl  = 0x11
	VKMenu     = 0x12 // Alt
	VKCapital  = 0x14 // Caps Lock
	VKEscape   = 0x1B
	VKSpace    = 0x20
	VKDelete   = 0x2E
	VKLWin     = 0x5B
	VKRWin     = 0x5C
	VKNumpad0  = 0x60
	VKNumpad9  = 0x69
	VKMultiply = 0x6A
	VKAdd      = 0x6B
	VKSubtract = 0x6D
	VKDecimal  = 0x6E
	VKDivide   = 0x6F
	VKF4       = 0x73
	VKLShift   = 0xA0
	VKRShift   = 0xA1
	VKLControl = 0xA2
	VKRControl = 0xA3
	VKLMenu    = 0xA4
	VKRMenu    = 0xA5
	VKPacket   = 0xE7 // a Unicode character sent with KEYEVENTF_UNICODE; the hook gets it in scanCode
)

// The shortcut: Ctrl+Alt+Shift+L locks when unlocked and opens the unlock
// prompt when locked. It never unlocks by itself.
const (
	HotkeyVK    = 'L'
	HotkeyLabel = "Ctrl+Alt+Shift+L"
)

// canonical folds the generic Shift, Ctrl and Alt codes that software may
// inject into the left-hand codes physical keys report, so a press and its
// release are tracked as one key.
func canonical(vk uint32) uint32 {
	switch vk {
	case VKShift:
		return VKLShift
	case VKControl:
		return VKLControl
	case VKMenu:
		return VKLMenu
	}
	return vk
}

var oemKeys = map[uint32][2]byte{
	0xBA: {';', ':'}, 0xBB: {'=', '+'}, 0xBC: {',', '<'}, 0xBD: {'-', '_'},
	0xBE: {'.', '>'}, 0xBF: {'/', '?'}, 0xC0: {'`', '~'}, 0xDB: {'[', '{'},
	0xDC: {'\\', '|'}, 0xDD: {']', '}'}, 0xDE: {'\'', '"'}, 0xE2: {'\\', '|'},
}

// TypedChar is the character a key types on the US English layout, or 0. The
// passphrase is printable ASCII, so the prompt reads keys this way whatever
// layout Windows has selected, as the Mac app asks for an English layout.
func TypedChar(vk uint32, shift, capsLock bool) byte {
	switch {
	case vk >= 'A' && vk <= 'Z':
		if shift != capsLock {
			return byte(vk)
		}
		return byte(vk) + ('a' - 'A')
	case vk >= '0' && vk <= '9':
		if shift {
			return ")!@#$%^&*("[vk-'0']
		}
		return byte(vk)
	case vk == VKSpace:
		return ' '
	case vk >= VKNumpad0 && vk <= VKNumpad9:
		return byte('0' + vk - VKNumpad0)
	case vk == VKMultiply:
		return '*'
	case vk == VKAdd:
		return '+'
	case vk == VKSubtract:
		return '-'
	case vk == VKDecimal:
		return '.'
	case vk == VKDivide:
		return '/'
	}
	if pair, ok := oemKeys[vk]; ok {
		if shift {
			return pair[1]
		}
		return pair[0]
	}
	return 0
}
