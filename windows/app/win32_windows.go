//go:build windows

package main

// The few Win32 functions, constants and structures the app uses, called
// through the standard syscall package: no cgo, no third-party packages.

import (
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	crypt32  = syscall.NewLazyDLL("crypt32.dll")
	wtsapi32 = syscall.NewLazyDLL("wtsapi32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")

	pSetDefaultDllDirectories = kernel32.NewProc("SetDefaultDllDirectories")
	pGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
	pCreateMutexW             = kernel32.NewProc("CreateMutexW")
	pGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
	pSetThreadExecutionState  = kernel32.NewProc("SetThreadExecutionState")
	pLocalFree                = kernel32.NewProc("LocalFree")
	pGetCurrentThread         = kernel32.NewProc("GetCurrentThread")
	pSetThreadPriority        = kernel32.NewProc("SetThreadPriority")

	pRegisterClassExW            = user32.NewProc("RegisterClassExW")
	pCreateWindowExW             = user32.NewProc("CreateWindowExW")
	pDefWindowProcW              = user32.NewProc("DefWindowProcW")
	pDestroyWindow               = user32.NewProc("DestroyWindow")
	pShowWindow                  = user32.NewProc("ShowWindow")
	pGetMessageW                 = user32.NewProc("GetMessageW")
	pPeekMessageW                = user32.NewProc("PeekMessageW")
	pTranslateMessage            = user32.NewProc("TranslateMessage")
	pDispatchMessageW            = user32.NewProc("DispatchMessageW")
	pIsDialogMessageW            = user32.NewProc("IsDialogMessageW")
	pPostMessageW                = user32.NewProc("PostMessageW")
	pPostThreadMessageW          = user32.NewProc("PostThreadMessageW")
	pPostQuitMessage             = user32.NewProc("PostQuitMessage")
	pSendMessageW                = user32.NewProc("SendMessageW")
	pSetWindowsHookExW           = user32.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx         = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx              = user32.NewProc("CallNextHookEx")
	pRegisterHotKey              = user32.NewProc("RegisterHotKey")
	pUnregisterHotKey            = user32.NewProc("UnregisterHotKey")
	pSetTimer                    = user32.NewProc("SetTimer")
	pKillTimer                   = user32.NewProc("KillTimer")
	pGetAsyncKeyState            = user32.NewProc("GetAsyncKeyState")
	pGetKeyState                 = user32.NewProc("GetKeyState")
	pLoadCursorW                 = user32.NewProc("LoadCursorW")
	pCreatePopupMenu             = user32.NewProc("CreatePopupMenu")
	pAppendMenuW                 = user32.NewProc("AppendMenuW")
	pTrackPopupMenu              = user32.NewProc("TrackPopupMenu")
	pDestroyMenu                 = user32.NewProc("DestroyMenu")
	pSetForegroundWindow         = user32.NewProc("SetForegroundWindow")
	pGetForegroundWindow         = user32.NewProc("GetForegroundWindow")
	pGetCursorPos                = user32.NewProc("GetCursorPos")
	pSetWindowPos                = user32.NewProc("SetWindowPos")
	pGetSystemMetrics            = user32.NewProc("GetSystemMetrics")
	pGetSystemMetricsForDpi      = user32.NewProc("GetSystemMetricsForDpi")
	pGetDpiForWindow             = user32.NewProc("GetDpiForWindow")
	pGetDpiForSystem             = user32.NewProc("GetDpiForSystem")
	pBeginPaint                  = user32.NewProc("BeginPaint")
	pEndPaint                    = user32.NewProc("EndPaint")
	pInvalidateRect              = user32.NewProc("InvalidateRect")
	pGetClientRect               = user32.NewProc("GetClientRect")
	pFillRect                    = user32.NewProc("FillRect")
	pDrawTextW                   = user32.NewProc("DrawTextW")
	pSetLayeredWindowAttributes  = user32.NewProc("SetLayeredWindowAttributes")
	pSetWindowRgn                = user32.NewProc("SetWindowRgn")
	pMonitorFromPoint            = user32.NewProc("MonitorFromPoint")
	pGetMonitorInfoW             = user32.NewProc("GetMonitorInfoW")
	pGetWindowTextW              = user32.NewProc("GetWindowTextW")
	pSetWindowTextW              = user32.NewProc("SetWindowTextW")
	pEnableWindow                = user32.NewProc("EnableWindow")
	pMessageBoxW                 = user32.NewProc("MessageBoxW")
	pCreateIconIndirect          = user32.NewProc("CreateIconIndirect")
	pDestroyIcon                 = user32.NewProc("DestroyIcon")
	pDrawIconEx                  = user32.NewProc("DrawIconEx")
	pGetDC                       = user32.NewProc("GetDC")
	pReleaseDC                   = user32.NewProc("ReleaseDC")
	pIsWindow                    = user32.NewProc("IsWindow")
	pRegisterWindowMessageW      = user32.NewProc("RegisterWindowMessageW")
	pFindWindowW                 = user32.NewProc("FindWindowW")
	pSetFocus                    = user32.NewProc("SetFocus")
	pGetPointerType              = user32.NewProc("GetPointerType")
	pSetWinEventHook             = user32.NewProc("SetWinEventHook")
	pUnhookWinEvent              = user32.NewProc("UnhookWinEvent")
	pChangeWindowMessageFilterEx = user32.NewProc("ChangeWindowMessageFilterEx")
	pAdjustWindowRectExForDpi    = user32.NewProc("AdjustWindowRectExForDpi")
	pGetSysColor                 = user32.NewProc("GetSysColor")
	pGetSysColorBrush            = user32.NewProc("GetSysColorBrush")
	pFlashWindow                 = user32.NewProc("FlashWindow")
	pGetDlgItem                  = user32.NewProc("GetDlgItem")
	pGetTopWindow                = user32.NewProc("GetTopWindow")
	pGetWindow                   = user32.NewProc("GetWindow")
	pIsWindowVisible             = user32.NewProc("IsWindowVisible")
	pIsIconic                    = user32.NewProc("IsIconic")
	pGetWindowLongPtrW           = user32.NewProc("GetWindowLongPtrW")
	pGetClassNameW               = user32.NewProc("GetClassNameW")

	pCreateFontW        = gdi32.NewProc("CreateFontW")
	pSelectObject       = gdi32.NewProc("SelectObject")
	pDeleteObject       = gdi32.NewProc("DeleteObject")
	pSetBkMode          = gdi32.NewProc("SetBkMode")
	pSetBkColor         = gdi32.NewProc("SetBkColor")
	pSetTextColor       = gdi32.NewProc("SetTextColor")
	pCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pCreateBitmap       = gdi32.NewProc("CreateBitmap")
	pCreateRoundRectRgn = gdi32.NewProc("CreateRoundRectRgn")
	pGetStockObject     = gdi32.NewProc("GetStockObject")

	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pShellExecuteW    = shell32.NewProc("ShellExecuteW")
	pIsUserAnAdmin    = shell32.NewProc("IsUserAnAdmin")

	pCryptProtectData   = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")

	pWTSRegisterSessionNotification   = wtsapi32.NewProc("WTSRegisterSessionNotification")
	pWTSUnRegisterSessionNotification = wtsapi32.NewProc("WTSUnRegisterSessionNotification")

	pDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")

	pInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
)

const (
	loadLibrarySearchSystem32 = 0x00000800

	wmNull             = 0x0000
	wmDestroy          = 0x0002
	wmPaint            = 0x000F
	wmClose            = 0x0010
	wmQueryEndSession  = 0x0011
	wmQuit             = 0x0012
	wmEraseBkgnd       = 0x0014
	wmEndSession       = 0x0016
	wmSettingChange    = 0x001A
	wmMouseActivate    = 0x0021
	wmSetFont          = 0x0030
	wmDisplayChange    = 0x007E
	wmNCHitTest        = 0x0084
	wmCommand          = 0x0111
	wmTimer            = 0x0113
	wmCtlColorStatic   = 0x0138
	wmMouseFirst       = 0x0200
	wmMouseMove        = 0x0200
	wmLButtonDown      = 0x0201
	wmLButtonUp        = 0x0202
	wmRButtonDown      = 0x0204
	wmRButtonUp        = 0x0205
	wmMButtonDown      = 0x0207
	wmMButtonUp        = 0x0208
	wmMouseWheel       = 0x020A
	wmXButtonDown      = 0x020B
	wmXButtonUp        = 0x020C
	wmMouseHWheel      = 0x020E
	wmMouseLast        = 0x020E
	wmPointerUpdate    = 0x0245
	wmPointerDown      = 0x0246
	wmPointerUp        = 0x0247
	wmPointerActivate  = 0x024B
	wmWTSSessionChange = 0x02B1
	wmDPIChanged       = 0x02E0
	wmHotkey           = 0x0312
	wmUser             = 0x0400
	wmApp              = 0x8000

	wsPopup        = 0x80000000
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsClipChildren = 0x02000000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsTabStop      = 0x00010000

	wsExTopmost       = 0x00000008
	wsExTransparent   = 0x00000020
	wsExToolWindow    = 0x00000080
	wsExClientEdge    = 0x00000200
	wsExControlParent = 0x00010000
	wsExLayered       = 0x00080000
	wsExNoActivate    = 0x08000000

	ssNoPrefix      = 0x0080
	bsPushButton    = 0x0000
	bsDefPushButton = 0x0001
	bsAutoCheckbox  = 0x0003
	esPassword      = 0x0020
	esAutoHScroll   = 0x0080
	bmGetCheck      = 0x00F0
	bmSetCheck      = 0x00F1
	emLimitText     = 0x00C5

	swHide       = 0
	swShowNormal = 1
	swShow       = 5

	swpNoSize         = 0x0001
	swpNoMove         = 0x0002
	swpNoZOrder       = 0x0004
	swpNoActivate     = 0x0010
	swpShowWindow     = 0x0040
	hwndTopmost       = ^uintptr(0) // (HWND)-1
	smCXSmIcon        = 49
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	lwaAlpha = 0x2

	whKeyboardLL = 13
	whMouseLL    = 14
	// Injected by software. The lower-integrity bits are also set alone by
	// some remote-control apps, so either counts.
	llkhfInjectedAny   = 0x10 | 0x02 // LLKHF_INJECTED | LLKHF_LOWER_IL_INJECTED
	llkhfUp            = 0x80
	llmhfInjectedAny   = 0x01 | 0x02 // LLMHF_INJECTED | LLMHF_LOWER_IL_INJECTED
	touchSignatureMask = 0xFFFFFF00
	touchSignature     = 0xFF515700 // MI_WP_SIGNATURE: Windows made this click from a touch or pen

	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modNoRepeat = 0x4000

	nimAdd     = 0
	nimModify  = 1
	nimDelete  = 2
	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	mfString       = 0x0000
	mfGrayed       = 0x0001
	mfSeparator    = 0x0800
	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	esContinuous      = 0x80000000
	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002

	wtsConsoleConnect    = 0x1
	wtsConsoleDisconnect = 0x2
	wtsRemoteConnect     = 0x3
	wtsRemoteDisconnect  = 0x4
	wtsSessionLogoff     = 0x6
	wtsSessionLock       = 0x7
	wtsSessionUnlock     = 0x8

	eventSystemDesktopSwitch = 0x0020
	maNoActivate             = 3
	paNoActivate             = 3
	ptTouch                  = 2
	ptPen                    = 3

	dtWordBreak  = 0x0010
	dtSingleLine = 0x0020
	dtCalcRect   = 0x0400
	dtNoPrefix   = 0x0800

	colorWindow                 = 5
	colorWindowText             = 8
	colorGrayText               = 17
	idcArrow                    = 32512
	diNormal                    = 0x0003
	monitorDefaultToPrimary     = 1
	cryptProtectUIForbidden     = 0x1
	msgfltAllow                 = 1
	errorAlreadyExists          = 183
	errorAccessDenied           = 5
	dwmwaWindowCornerPreference = 33
	dwmwcpRound                 = 2
	iccStandardClasses          = 0x00004000
	mbOK                        = 0x00000000
	mbIconWarning               = 0x00000030
	mbIconInfo                  = 0x00000040
	mbSetForeground             = 0x00010000
	idOK                        = 1

	gwlExStyle = ^uintptr(19) // GWL_EXSTYLE, -20
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type paintStruct struct {
	HDC         uintptr
	Erase       int32
	Paint       rect
	Restore     int32
	IncUpdate   int32
	RGBReserved [32]byte
}

type kbdllHookStruct struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type msllHookStruct struct {
	Pt        point
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type notifyIconData struct {
	Size            uint32
	HWnd            uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        guid
	BalloonIcon     uintptr
}

type iconInfo struct {
	Icon     int32
	XHotspot uint32
	YHotspot uint32
	Mask     uintptr
	Color    uintptr
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type dataBlob struct {
	Size uint32
	Data *byte
}

type initCommonControlsEx struct {
	Size uint32
	ICC  uint32
}

// call runs a Win32 function and returns its first result. Like
// syscall.LazyProc.Call itself, it is marked uintptrescapes: Go memory passed
// as uintptr(unsafe.Pointer(...)) in its arguments moves to the heap and stays
// alive until the call returns. Without that, a buffer on a goroutine's stack
// could move while Windows still writes to it, for example when GetMessage
// calls back into Go for a hook or a window.
//
//go:uintptrescapes
func call(p *syscall.LazyProc, args ...uintptr) uintptr {
	r, _, _ := p.Call(args...)
	return r
}

func wstr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("?")
	}
	return p
}

// wsz is a string as Windows wants it. Convert it with uintptr(wsz(s)) inside
// the call's own arguments, so the text stays alive during the call.
func wsz(s string) unsafe.Pointer { return unsafe.Pointer(wstr(s)) }

func loword(v uintptr) uint32 { return uint32(v) & 0xFFFF }
func hiword(v uintptr) uint32 { return (uint32(v) >> 16) & 0xFFFF }

func rgb(r, g, b uint8) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

func postMessage(hwnd uintptr, message uint32, wParam, lParam uintptr) {
	call(pPostMessageW, hwnd, uintptr(message), wParam, lParam)
}

func sendMessage(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	return call(pSendMessageW, hwnd, uintptr(message), wParam, lParam)
}

func setWindowPos(hwnd, after uintptr, x, y, w, h int32, flags uint32) {
	call(pSetWindowPos, hwnd, after, uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(flags))
}

func windowText(hwnd uintptr) string {
	buf := make([]uint16, 160)
	n := call(pGetWindowTextW, hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	text := syscall.UTF16ToString(buf[:n])
	clear(buf)
	return text
}

func dpiOf(hwnd uintptr) uint32 {
	if hwnd != 0 && pGetDpiForWindow.Find() == nil {
		if d := uint32(call(pGetDpiForWindow, hwnd)); d > 0 {
			return d
		}
	}
	if pGetDpiForSystem.Find() == nil {
		if d := uint32(call(pGetDpiForSystem)); d > 0 {
			return d
		}
	}
	return 96
}

func scale(v int, dpi uint32) int32 { return int32((v*int(dpi) + 48) / 96) }

func messageBox(text, title string, flags uintptr) {
	call(pMessageBoxW, 0, uintptr(wsz(text)), uintptr(wsz(title)), flags|mbSetForeground)
}

func createFont(size int, weight int, dpi uint32) uintptr {
	height := -int32((size*int(dpi) + 36) / 72) // points to pixels
	return call(pCreateFontW, uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1 /* DEFAULT_CHARSET */, 0, 0, 5 /* CLEARTYPE_QUALITY */, 0, uintptr(wsz("Segoe UI")))
}

// lightTheme reads Windows' app color setting; light when it can't tell.
func lightTheme() bool {
	var key syscall.Handle
	path := wstr(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, path, 0, syscall.KEY_READ, &key) != nil {
		return true
	}
	defer syscall.RegCloseKey(key)
	var value, kind uint32
	size := uint32(4)
	if syscall.RegQueryValueEx(key, wstr("AppsUseLightTheme"), nil, &kind, (*byte)(unsafe.Pointer(&value)), &size) != nil || kind != syscall.REG_DWORD {
		return true
	}
	return value != 0
}

func createWindow(exStyle uint32, class, title string, style uint32, x, y, w, h int32, parent, id uintptr) uintptr {
	return call(pCreateWindowExW, uintptr(exStyle), uintptr(wsz(class)), uintptr(wsz(title)), uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, id, instance, 0)
}

func registerClass(name string, proc uintptr, background uintptr) {
	class := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:    proc,
		Instance:   instance,
		Cursor:     call(pLoadCursorW, 0, idcArrow),
		Background: background,
		ClassName:  wstr(name),
	}
	call(pRegisterClassExW, uintptr(unsafe.Pointer(&class)))
}
