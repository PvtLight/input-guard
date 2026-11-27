//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"inputguard/art"
	"inputguard/core"
)

const (
	wmTray         = wmApp + 1
	wmGuard        = wmApp + 2 // from the hooks: the prompt changed
	wmVerified     = wmApp + 3 // from the passphrase check
	wmShowSettings = wmApp + 4 // from a second copy of the app
	wmSaved        = wmApp + 5 // from saving a passphrase
	wmRebuild      = wmApp + 6 // to the settings window: lay it out again

	hotkeyID         = 1
	healthTimer      = 1
	inputCheckTimer  = 2
	sessionRetry     = 3
	trayID           = 1
	inputCheckLength = 10 * time.Second

	cmdLock = iota + 100
	cmdTrial
	cmdSettings
	cmdQuit
	cmdUnlock
)

var (
	instance       uintptr
	mainWindow     uintptr
	taskbarCreated uint32
	a              *app
)

type app struct {
	exe          string
	vault        core.Vault
	settingsPath string
	options      core.Settings
	elevated     bool
	hotkey       bool // the shortcut is registered
	sessionWatch bool // Windows tells us about locks and user switches
	locked       bool
	activating   bool
	trial        time.Duration // when the trial ends, or 0
	record       *core.Record
	recovery     *core.Supervisor
	hooks        *hookThread
	desktopHook  uintptr
	awake        bool
	tray         bool
	checking     bool // the input check is running
	status       string
	warning      bool
	icons        map[[2]int]uintptr
	settings     settingsWindow
	notice       notice
	shield       shield
	saves        chan saveResult
}

var mainCallback = syscall.NewCallback(mainProc)

func runApp() {
	instance = call(pGetModuleHandleW, 0)
	controls := initCommonControlsEx{uint32(unsafe.Sizeof(initCommonControlsEx{})), iccStandardClasses}
	call(pInitCommonControlsEx, uintptr(unsafe.Pointer(&controls)))
	exe, err := os.Executable()
	if err != nil {
		messageBox("Input Guard could not find its own file.", "Input Guard", mbIconWarning)
		return
	}
	dir := filepath.Join(localAppData(), "Input Guard")
	a = &app{
		exe:          exe,
		vault:        core.Vault{Path: filepath.Join(dir, "passphrase.dat"), Protector: dpapi{}},
		settingsPath: filepath.Join(dir, "settings.json"),
		elevated:     uint32(call(pIsUserAnAdmin)) != 0,
		icons:        map[[2]int]uintptr{},
		saves:        make(chan saveResult, 4),
	}
	a.options = core.ReadSettings(a.settingsPath)

	registerClass("InputGuardMain", mainCallback, 0)
	registerClass("InputGuardSettings", settingsCallback, call(pGetSysColorBrush, colorWindow))
	registerClass("InputGuardNotice", noticeCallback, 0)
	registerClass("InputGuardShield", shieldCallback, call(pGetStockObject, 4)) // BLACK_BRUSH
	mainWindow = createWindow(0, "InputGuardMain", "Input Guard", wsPopup, 0, 0, 0, 0, 0, 0)
	if mainWindow == 0 {
		messageBox("Input Guard could not start.", "Input Guard", mbIconWarning)
		return
	}
	taskbarCreated = uint32(call(pRegisterWindowMessageW, uintptr(wsz("TaskbarCreated"))))
	// Running as administrator, Windows filters messages from normal apps
	// such as the taskbar unless they are allowed.
	for _, m := range []uint32{taskbarCreated, wmTray, wmShowSettings} {
		call(pChangeWindowMessageFilterEx, mainWindow, uintptr(m), msgfltAllow, 0)
	}
	a.watchSessions()
	a.hotkey = uint32(call(pRegisterHotKey, mainWindow, hotkeyID, modControl|modAlt|modShift|modNoRepeat, core.HotkeyVK)) != 0
	a.addTray()
	a.showSettings()

	var m msg
	for {
		r := int32(call(pGetMessageW, uintptr(unsafe.Pointer(&m)), 0, 0, 0))
		if r == 0 || r == -1 {
			break
		}
		if a.settings.hwnd != 0 && uint32(call(pIsDialogMessageW, a.settings.hwnd, uintptr(unsafe.Pointer(&m)))) != 0 {
			continue
		}
		call(pTranslateMessage, uintptr(unsafe.Pointer(&m)))
		call(pDispatchMessageW, uintptr(unsafe.Pointer(&m)))
	}
}

func localAppData() string {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return dir
	}
	dir, _ := os.UserCacheDir()
	return dir
}

// watchSessions asks Windows for lock, unlock and user-switch notices. They
// end the guard, which is what makes Ctrl+Alt+Del → Lock a way back in.
func (a *app) watchSessions() {
	if !a.sessionWatch {
		a.sessionWatch = uint32(call(pWTSRegisterSessionNotification, mainWindow, 0)) != 0 // NOTIFY_FOR_THIS_SESSION
		if !a.sessionWatch {
			call(pSetTimer, mainWindow, sessionRetry, 5000, 0)
		}
	}
}

func mainProc(hwnd, message, wParam, lParam uintptr) uintptr {
	m := uint32(message)
	if m == taskbarCreated && taskbarCreated != 0 {
		a.tray = false
		a.addTray() // Explorer restarted
		return 0
	}
	switch m {
	case wmTray:
		if l := uint32(lParam); l == wmLButtonUp || l == wmRButtonUp {
			a.showMenu()
		}
		return 0
	case wmHotkey:
		if a.locked {
			a.openPrompt()
		} else {
			a.beginLock(false)
		}
		return 0
	case wmGuard:
		a.guardChanged(wParam)
		return 0
	case wmVerified:
		a.verified(uint64(wParam), lParam != 0)
		return 0
	case wmShowSettings:
		a.showSettings()
		return 0
	case wmSaved:
		select {
		case result := <-a.saves:
			a.settings.saved(result)
		default:
		}
		return 0
	case wmTimer:
		switch wParam {
		case healthTimer:
			a.checkHealth()
		case inputCheckTimer:
			a.finishInputCheck()
		case sessionRetry:
			call(pKillTimer, mainWindow, sessionRetry)
			a.watchSessions()
		}
		return 0
	case wmWTSSessionChange:
		switch wParam {
		case wtsConsoleConnect, wtsConsoleDisconnect, wtsRemoteConnect, wtsRemoteDisconnect,
			wtsSessionLogoff, wtsSessionLock, wtsSessionUnlock:
			if a.locked {
				a.endLock()
				a.setStatus("Windows was locked or switched users, so Input Guard turned off. Lock again when you're ready.", false)
			}
		}
		return 0
	case wmQueryEndSession:
		return 1 // never hold up shutdown or sign-out
	case wmEndSession:
		if wParam != 0 {
			a.endLock()
			a.removeTray()
		}
		return 0
	case wmDisplayChange, wmSettingChange:
		if a.shield.raised {
			a.shield.raise()
		}
		if a.tray {
			a.updateTray()
		}
		return 0
	}
	return call(pDefWindowProcW, hwnd, message, wParam, lParam)
}

// setStatus shows a message at the top of the settings window. The window
// is rebuilt a moment later, never inside a click on one of its own buttons.
func (a *app) setStatus(text string, warning bool) {
	a.status, a.warning = text, warning
	if a.settings.hwnd != 0 {
		postMessage(a.settings.hwnd, wmRebuild, 0, 0)
	}
}

// beginLock starts a lock, or a 15-second trial. Every step must succeed
// before input is blocked; any failure undoes the earlier steps.
func (a *app) beginLock(trial bool) {
	if a.locked || a.activating || a.checking {
		return
	}
	if a.settings.saving {
		// The new passphrase isn't saved yet; locking now would use the old one.
		a.setStatus("Wait for the passphrase to finish saving, then lock.", false)
		return
	}
	a.activating = true
	defer func() { a.activating = false }()
	if err := a.startLock(trial); err != nil {
		a.endLock()
		a.setStatus(err.Error(), true)
		a.showSettings()
	}
}

func (a *app) startLock(trial bool) error {
	a.watchSessions()
	if !a.sessionWatch {
		return errors.New("Windows isn't reporting screen locks to Input Guard yet, so Ctrl+Alt+Del → Lock could not end the guard. Wait a moment and try again.")
	}
	record, err := a.vault.Read()
	if err != nil {
		return err
	}
	if record == nil {
		return errors.New("Set your Input Guard passphrase below before locking.")
	}
	length := time.Duration(0)
	if trial {
		length = core.TrialLength
	}
	// The recovery helper comes first, as on the Mac: no helper, no lock.
	recovery, err := core.StartRecovery(func(arg string) *exec.Cmd {
		return exec.Command(a.exe, "--recovery", arg)
	}, length, core.SystemClock)
	if err != nil {
		return err
	}
	a.locked = true // from here on, endLock undoes whatever was started
	a.record = record
	a.recovery = recovery
	hooks, err := startHooks()
	if err != nil {
		return err
	}
	a.hooks = hooks
	automation := a.options.AllowAutomation
	// The hooks are in place and pass everything until the guard locks. Apps
	// already saw the keys held now, such as the shortcut's Ctrl+Alt+Shift,
	// so their releases will still pass.
	shared.Lock()
	shared.guard.Lock(heldKeys(), capsLockOn(), automation)
	shared.Unlock()
	if trial {
		a.trial = recovery.Deadline()
	}
	// Touches never reach the hooks, so cover the screens. Not with
	// automation allowed: the cover would also catch the clicks it lets through.
	if !automation && !a.shield.raise() {
		return errors.New("Could not cover the screens to block the touch screen. Lock was not started.")
	}
	if uint32(call(pSetThreadExecutionState, esContinuous|esSystemRequired|esDisplayRequired)) == 0 {
		return errors.New("Could not keep the PC awake. Lock was not started.")
	}
	a.awake = true
	// Back from Windows' own screens (Ctrl+Alt+Del, UAC), forget keys whose
	// release happened there.
	a.desktopHook = call(pSetWinEventHook, eventSystemDesktopSwitch, eventSystemDesktopSwitch, 0, desktopSwitchCallback, 0, 0, 0)
	// The hooks briefly share the guard's state with this thread. A busy PC
	// mustn't hold this thread up while it holds that state.
	call(pSetThreadPriority, call(pGetCurrentThread), 1) // THREAD_PRIORITY_ABOVE_NORMAL
	call(pSetTimer, mainWindow, healthTimer, 250, 0)
	a.settings.close()
	a.updateTray()
	a.notice.show()
	return nil
}

// endLock gives input back first, then takes everything else down. The
// helper stays armed until the hooks are gone.
func (a *app) endLock() {
	shared.Lock()
	shared.guard.Unlock()
	shared.Unlock()
	if a.hooks != nil {
		a.hooks.stop()
		a.hooks = nil
	}
	a.shield.lower()
	if a.recovery != nil {
		a.recovery.Stop()
		a.recovery = nil
	}
	if a.desktopHook != 0 {
		call(pUnhookWinEvent, a.desktopHook)
		a.desktopHook = 0
	}
	if a.awake {
		call(pSetThreadExecutionState, esContinuous)
		a.awake = false
	}
	call(pKillTimer, mainWindow, healthTimer)
	call(pSetThreadPriority, call(pGetCurrentThread), 0) // THREAD_PRIORITY_NORMAL
	a.locked = false
	a.trial = 0
	a.record = nil
	a.notice.hide()
	a.updateTray()
}

var desktopSwitchCallback = syscall.NewCallback(func(hook, event, hwnd, object, child, thread, when uintptr) uintptr {
	shared.Lock()
	shared.guard.ResetHeld()
	shared.Unlock()
	return 0
})

func (a *app) checkHealth() {
	if !a.locked {
		return
	}
	if a.trial > 0 && core.SystemClock.Now() >= a.trial-250*time.Millisecond {
		a.endLock()
		a.setStatus("The 15-second trial finished. Input is enabled again.", false)
		a.showSettings()
		return
	}
	if a.recovery == nil || !a.recovery.Pulse() || a.hooks == nil || !a.hooks.healthy() {
		a.endLock()
		a.setStatus("Input Guard stopped because an input or recovery check failed. Your input is NOT locked. Quit and reopen Input Guard before trying again.", true)
		a.showSettings()
		call(pFlashWindow, a.settings.hwnd, 1)
		return
	}
	if a.shield.raised {
		a.shield.raise() // stay above other always-on-top windows, on every screen
	}
	a.notice.tick()
	if a.trial > 0 {
		a.updateTray()
	}
}

// hookLost ends the lock when the cover catches a click the mouse hook
// should have blocked: Windows drops a hook that once took too long.
func (a *app) hookLost() {
	if !a.locked {
		return
	}
	a.endLock()
	a.setStatus("Windows stopped Input Guard's input blocker, so the lock ended. Your input is NOT locked. Quit and reopen Input Guard before trying again.", true)
	a.showSettings()
	call(pFlashWindow, a.settings.hwnd, 1)
}

func (a *app) guardChanged(flags uintptr) {
	if !a.locked {
		return
	}
	if flags&(noticeShow|noticeCounted) != 0 {
		a.notice.show() // a counted click brings the notice up with one more dot
	}
	if flags&noticeHide != 0 {
		if a.trial > 0 {
			a.notice.show() // back to the trial's countdown
		} else {
			a.notice.hide()
		}
	}
	if flags&noticeVerify != 0 {
		a.verify()
	}
	if flags&(noticeRedraw|noticeVerify) != 0 {
		a.notice.redraw()
	}
}

func (a *app) openPrompt() {
	if !a.locked {
		return
	}
	shared.Lock()
	opened := shared.guard.OpenPrompt(core.SystemClock.Now())
	shared.Unlock()
	if opened {
		a.notice.show()
	}
}

// verify checks the passphrase off the UI thread: 600,000 rounds take a moment.
func (a *app) verify() {
	shared.Lock()
	candidate, session := shared.guard.TakeSubmission()
	shared.Unlock()
	record := a.record
	go func() {
		matched := record != nil && record.Matches(candidate)
		var flag uintptr
		if matched {
			flag = 1
		}
		postMessage(mainWindow, wmVerified, uintptr(session), flag)
	}()
}

func (a *app) verified(session uint64, matched bool) {
	shared.Lock()
	unlock := shared.guard.Verified(session, matched, core.SystemClock.Now())
	shared.Unlock()
	if !unlock {
		a.notice.redraw()
		return
	}
	wasTrial := a.trial > 0
	a.endLock()
	if wasTrial {
		a.setStatus("Your passphrase unlocked the trial. Input is enabled again.", false)
		a.showSettings()
	}
}

// heldKeys lists the keys and mouse buttons down right now, as apps see them.
func heldKeys() []uint32 {
	var down []uint32
	for vk := uint32(1); vk < 255; vk++ {
		switch vk {
		case core.VKShift, core.VKControl, core.VKMenu, core.VKPacket:
			continue // the left and right keys are read on their own
		}
		if uint16(call(pGetAsyncKeyState, uintptr(vk)))&0x8000 != 0 {
			down = append(down, vk)
		}
	}
	return down
}

func capsLockOn() bool { return uint16(call(pGetKeyState, core.VKCapital))&1 != 0 }

// Notification-area icon and menu.

func (a *app) icon(size int, locked bool) uintptr {
	key := [2]int{size, 0}
	if locked {
		key[1] = 1
	}
	if h, ok := a.icons[key]; ok {
		return h
	}
	h := makeIcon(art.TrayIcon(size, locked))
	a.icons[key] = h
	return h
}

func (a *app) brand(size int) uintptr {
	key := [2]int{size, 2}
	if h, ok := a.icons[key]; ok {
		return h
	}
	h := makeIcon(art.BrandMark(size))
	a.icons[key] = h
	return h
}

func trayIconSize() int {
	size := 16
	if pGetSystemMetricsForDpi.Find() == nil {
		size = int(int32(call(pGetSystemMetricsForDpi, smCXSmIcon, uintptr(dpiOf(0)))))
	} else {
		size = int(int32(call(pGetSystemMetrics, smCXSmIcon)))
	}
	return max(16, min(size, 64))
}

func (a *app) trayData() notifyIconData {
	nid := notifyIconData{HWnd: mainWindow, ID: trayID}
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.Icon = a.icon(trayIconSize(), a.locked)
	tip := "Input Guard: unlocked"
	if a.locked {
		tip = "Input Guard: Agent working"
		if a.trial > 0 {
			tip = "Input Guard: trial, " + core.Seconds(a.trial-core.SystemClock.Now()) + " left"
		}
	}
	copy(nid.Tip[:len(nid.Tip)-1], utf16.Encode([]rune(tip)))
	return nid
}

func (a *app) addTray() {
	nid := a.trayData()
	nid.Flags = nifMessage | nifIcon | nifTip
	nid.CallbackMessage = wmTray
	a.tray = uint32(call(pShellNotifyIconW, nimAdd, uintptr(unsafe.Pointer(&nid)))) != 0
}

func (a *app) updateTray() {
	nid := a.trayData()
	nid.Flags = nifIcon | nifTip
	ok := uint32(call(pShellNotifyIconW, nimModify, uintptr(unsafe.Pointer(&nid)))) != 0
	// A busy Explorer can report an add as failed and still show the icon,
	// so changing it is tried first; only then is it added.
	if !ok && !a.tray {
		a.addTray()
		return
	}
	a.tray = a.tray || ok
}

func (a *app) removeTray() {
	if a.tray {
		nid := a.trayData()
		call(pShellNotifyIconW, nimDelete, uintptr(unsafe.Pointer(&nid)))
		a.tray = false
	}
}

func appendMenu(menu uintptr, flags uint32, id int, text string) {
	call(pAppendMenuW, menu, uintptr(flags), uintptr(id), uintptr(wsz(text)))
}

func (a *app) showMenu() {
	menu := call(pCreatePopupMenu)
	defer call(pDestroyMenu, menu)
	if a.locked {
		state := "Agent working"
		if a.trial > 0 {
			state = "Trial: input returns in " + core.Seconds(a.trial-core.SystemClock.Now())
		}
		appendMenu(menu, mfString|mfGrayed, 0, state)
		appendMenu(menu, mfString, cmdUnlock, "Unlock…")
		appendMenu(menu, mfSeparator, 0, "")
		appendMenu(menu, mfString|mfGrayed, 0, "Quit (unlock first)")
	} else {
		appendMenu(menu, mfString, cmdLock, "Lock Input\t"+core.HotkeyLabel)
		appendMenu(menu, mfString, cmdTrial, "15-Second Trial")
		appendMenu(menu, mfSeparator, 0, "")
		appendMenu(menu, mfString, cmdSettings, "Settings…")
		appendMenu(menu, mfString, cmdQuit, "Quit Input Guard")
	}
	var pt point
	call(pGetCursorPos, uintptr(unsafe.Pointer(&pt)))
	previous := appWindow(call(pGetForegroundWindow))
	call(pSetForegroundWindow, mainWindow) // without this the menu doesn't close when clicking elsewhere
	choice := int(int32(call(pTrackPopupMenu, menu, tpmRightButton|tpmReturnCmd|tpmNoNotify,
		uintptr(pt.X), uintptr(pt.Y), 0, mainWindow, 0)))
	postMessage(mainWindow, wmNull, 0, 0)
	// Give the focus back to the app that had it, so an agent's window stays in front.
	restore := func() {
		if previous != 0 && previous != mainWindow && uint32(call(pIsWindow, previous)) != 0 {
			call(pSetForegroundWindow, previous)
		}
	}
	switch choice {
	case cmdLock:
		restore()
		a.beginLock(false)
	case cmdTrial:
		restore()
		a.beginLock(true)
	case cmdSettings:
		a.showSettings()
	case cmdQuit:
		a.quit()
	case cmdUnlock:
		restore()
		a.openPrompt()
	default:
		restore()
	}
}

// appWindow is the window to give the focus back to after the tray menu.
// Clicking the tray makes the taskbar the foreground window, so then it's
// the app window right behind it.
func appWindow(foreground uintptr) uintptr {
	if foreground != 0 && !shellWindow(foreground) && !ownWindow(foreground) {
		return foreground
	}
	for w := call(pGetTopWindow, 0); w != 0; w = call(pGetWindow, w, 2) { // GW_HWNDNEXT
		ex := uint32(call(pGetWindowLongPtrW, w, gwlExStyle))
		var cloaked uint32                                                        // on another virtual desktop, or a suspended app
		call(pDwmGetWindowAttribute, w, 14, uintptr(unsafe.Pointer(&cloaked)), 4) // DWMWA_CLOAKED
		if ownWindow(w) || shellWindow(w) || cloaked != 0 ||
			uint32(call(pIsWindowVisible, w)) == 0 || uint32(call(pIsIconic, w)) != 0 ||
			call(pGetWindow, w, 4) != 0 || // GW_OWNER: a popup that belongs to another window
			ex&(wsExToolWindow|wsExNoActivate|wsExTopmost) != 0 {
			continue
		}
		return w
	}
	return 0
}

func ownWindow(w uintptr) bool {
	return w == mainWindow || w == a.settings.hwnd || w == a.notice.hwnd || w == a.shield.hwnd
}

// shellWindow is the taskbar, its overflow area, or the desktop.
func shellWindow(w uintptr) bool {
	buf := make([]uint16, 64)
	n := call(pGetClassNameW, w, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	switch syscall.UTF16ToString(buf[:n]) {
	case "Shell_TrayWnd", "Shell_SecondaryTrayWnd", "NotifyIconOverflowWindow",
		"TopLevelWindowForOverflowXamlIsland", "Progman", "WorkerW":
		return true
	}
	return false
}

func (a *app) quit() {
	if a.locked || a.checking {
		return
	}
	a.removeTray()
	call(pUnregisterHotKey, mainWindow, hotkeyID)
	if a.sessionWatch {
		call(pWTSUnRegisterSessionNotification, mainWindow)
	}
	a.settings.close()
	call(pPostQuitMessage, 0)
}

// The input check: for 10 seconds, on request, the hooks count where input
// comes from (this PC's keyboard and mouse, or software such as Splashtop or
// an agent) and pass everything. Only counts and tags are kept, never keys.
type inputCounter struct {
	active                         atomic.Bool
	keysPC, keysSoftware, keysText atomic.Int64
	movesPC, movesSoftware         atomic.Int64
	clicksPC, clicksSoftware       atomic.Int64
	touch                          atomic.Int64
	mu                             sync.Mutex
	tags                           []uintptr
}

var inputCheck inputCounter

func (c *inputCounter) key(ev core.KeyEvent, tag uintptr) {
	switch {
	case !ev.Injected:
		c.keysPC.Add(1)
	default:
		c.keysSoftware.Add(1)
		if ev.VK == core.VKPacket {
			c.keysText.Add(1)
		}
		c.noteTag(tag)
	}
}

func (c *inputCounter) mouse(ev core.MouseEvent, tag uintptr) {
	if ev.Kind == core.MouseUp {
		return
	}
	click := ev.Kind == core.MouseDown
	switch {
	case ev.Touch:
		if click {
			c.touch.Add(1)
		}
	case !ev.Injected && click:
		c.clicksPC.Add(1)
	case !ev.Injected:
		c.movesPC.Add(1)
	case click:
		c.clicksSoftware.Add(1)
		c.noteTag(tag)
	default:
		c.movesSoftware.Add(1)
		c.noteTag(tag)
	}
}

// noteTag keeps the first few extra-info values software put on its input;
// a remote-control app may mark its own this way.
func (c *inputCounter) noteTag(tag uintptr) {
	if tag == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.tags {
		if t == tag {
			return
		}
	}
	if len(c.tags) < 4 {
		c.tags = append(c.tags, tag)
	}
}

func (c *inputCounter) reset() {
	for _, n := range []*atomic.Int64{&c.keysPC, &c.keysSoftware, &c.keysText, &c.movesPC, &c.movesSoftware,
		&c.clicksPC, &c.clicksSoftware, &c.touch} {
		n.Store(0)
	}
	c.mu.Lock()
	c.tags = nil
	c.mu.Unlock()
}

func (a *app) startInputCheck() {
	if a.locked || a.checking {
		return
	}
	inputCheck.reset()
	hooks, err := startHooks() // the guard is unlocked, so they pass everything
	if err != nil {
		a.setStatus(err.Error(), true)
		return
	}
	a.hooks = hooks
	a.checking = true
	inputCheck.active.Store(true)
	call(pSetTimer, mainWindow, inputCheckTimer, uintptr(inputCheckLength/time.Millisecond), 0)
	a.setStatus("Checking input for 10 seconds. Nothing is blocked. Type and click through Splashtop now, and on this PC's own keyboard and mouse if you're at it.", false)
}

func (a *app) finishInputCheck() {
	call(pKillTimer, mainWindow, inputCheckTimer)
	if !a.checking {
		return
	}
	inputCheck.active.Store(false)
	if a.hooks != nil {
		a.hooks.stop()
		a.hooks = nil
	}
	a.checking = false
	inputCheck.mu.Lock()
	var tags []string
	for _, t := range inputCheck.tags {
		tags = append(tags, fmt.Sprintf("0x%X", t))
	}
	inputCheck.mu.Unlock()
	tagText := "none"
	if len(tags) > 0 {
		tagText = strings.Join(tags, ", ")
	}
	report := fmt.Sprintf("Input check, 10 seconds:\n"+
		"• Keys: %d from this PC's keyboard, %d sent by software (%d of them as typed characters).\n"+
		"• Mouse: %d moves and %d clicks from this PC, %d moves and %d clicks sent by software. Touch clicks: %d.\n"+
		"• Tags on software input: %s.\n"+
		"Locked, all of it is blocked. With automation events allowed, what was sent by software passes.",
		inputCheck.keysPC.Load(), inputCheck.keysSoftware.Load(), inputCheck.keysText.Load(),
		inputCheck.movesPC.Load(), inputCheck.clicksPC.Load(), inputCheck.movesSoftware.Load(), inputCheck.clicksSoftware.Load(),
		inputCheck.touch.Load(), tagText)
	a.setStatus(report, false)
	a.showSettings()
}
