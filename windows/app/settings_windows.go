//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"

	"inputguard/core"
)

var settingsCallback = syscall.NewCallback(settingsProc)

const (
	idSave       = idOK // Enter in a passphrase box saves
	idCancel     = 2    // Esc closes the window
	idLock       = 201
	idTrial      = 202
	idCurrent    = 203
	idNew        = 204
	idAgain      = 205
	idAutomation = 206
	idCheck      = 207

	wmSetIcon      = 0x0080
	settingsWidth  = 880
	settingsStyle  = wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren
	settingsExtra  = wsExControlParent
	cwUseDefault   = -2147483648
	bnClicked      = 0
	warningColor   = 0x2020B0 // dark red, as 0x00BBGGRR
	secondaryColor = 0x505050
)

type saveResult struct {
	ok      bool
	message string
}

// settingsWindow is the window that opens at launch and from the tray menu:
// lock, trial, passphrase, automation events, the input check, and how to
// get back in. It is built from standard Windows controls.
type settingsWindow struct {
	hwnd     uintptr
	dpi      uint32
	fonts    [4]uintptr // title, heading, body, small
	children []uintptr
	gray     map[uintptr]bool
	warn     uintptr
	note     uintptr
	saveNote string
	saving   bool
	height   int32
}

func (a *app) showSettings() {
	if a.locked {
		return
	}
	s := &a.settings
	if s.hwnd == 0 {
		s.hwnd = createWindow(settingsExtra, "InputGuardSettings", "Input Guard", settingsStyle,
			cwUseDefault, cwUseDefault, 200, 200, 0, 0)
		if s.hwnd == 0 {
			return
		}
		sendMessage(s.hwnd, wmSetIcon, 0, a.brand(int(scale(16, dpiOf(s.hwnd)))))
		sendMessage(s.hwnd, wmSetIcon, 1, a.brand(int(scale(32, dpiOf(s.hwnd)))))
		s.build()
		s.center()
	}
	call(pShowWindow, s.hwnd, swShowNormal)
	call(pSetForegroundWindow, s.hwnd)
}

func (s *settingsWindow) close() {
	if s.hwnd != 0 {
		call(pDestroyWindow, s.hwnd)
	}
}

func (s *settingsWindow) rebuild() {
	if s.hwnd != 0 {
		s.build()
	}
}

func (s *settingsWindow) center() {
	var mi monitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	call(pGetMonitorInfoW, call(pMonitorFromPoint, 0, monitorDefaultToPrimary), uintptr(unsafe.Pointer(&mi)))
	w, h := s.outerSize()
	x := mi.Work.Left + (mi.Work.Right-mi.Work.Left-w)/2
	y := mi.Work.Top + max(0, (mi.Work.Bottom-mi.Work.Top-h)/2)
	setWindowPos(s.hwnd, 0, x, y, w, h, swpNoZOrder|swpNoActivate)
}

// outerSize is the window size that fits the content last built.
func (s *settingsWindow) outerSize() (int32, int32) {
	r := rect{0, 0, scale(settingsWidth, s.dpi), s.height}
	if pAdjustWindowRectExForDpi.Find() == nil {
		call(pAdjustWindowRectExForDpi, uintptr(unsafe.Pointer(&r)), settingsStyle, 0, settingsExtra, uintptr(s.dpi))
	}
	return r.Right - r.Left, r.Bottom - r.Top
}

func (s *settingsWindow) makeFonts(dpi uint32) {
	for _, f := range s.fonts {
		if f != 0 {
			call(pDeleteObject, f)
		}
	}
	s.dpi = dpi
	s.fonts = [4]uintptr{createFont(16, 600, dpi), createFont(11, 600, dpi), createFont(10, 400, dpi), createFont(9, 400, dpi)}
}

func (s *settingsWindow) measure(text string, font uintptr, width int32) int32 {
	dc := call(pGetDC, s.hwnd)
	defer call(pReleaseDC, s.hwnd, dc)
	old := call(pSelectObject, dc, font)
	r := rect{0, 0, width, 0}
	call(pDrawTextW, dc, uintptr(wsz(text)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), dtCalcRect|dtWordBreak|dtNoPrefix)
	call(pSelectObject, dc, old)
	return r.Bottom
}

func (s *settingsWindow) child(ex uint32, class, text string, style uint32, x, y, w, h int32, id int, font uintptr) uintptr {
	c := createWindow(ex, class, text, wsChild|wsVisible|style, x, y, w, h, s.hwnd, uintptr(id))
	sendMessage(c, wmSetFont, font, 0)
	s.children = append(s.children, c)
	return c
}

// build lays out every control for the window's current scale, in two
// columns so it fits a small laptop screen: locking and getting back in on
// the left, the passphrase and automation on the right.
func (s *settingsWindow) build() {
	for _, c := range s.children {
		call(pDestroyWindow, c)
	}
	s.children, s.gray, s.warn, s.note = nil, map[uintptr]bool{}, 0, 0
	if dpi := dpiOf(s.hwnd); dpi != s.dpi || s.fonts[0] == 0 {
		s.makeFonts(dpi)
	}
	heading, body, small := s.fonts[1], s.fonts[2], s.fonts[3]
	px := func(v int) int32 { return scale(v, s.dpi) }
	type column struct{ x, w, y int32 }
	text := func(c *column, t string, font uintptr, gray bool) uintptr {
		h := s.measure(t, font, c.w)
		label := s.child(0, "STATIC", t, ssNoPrefix, c.x, c.y, c.w, h, 0, font)
		if gray {
			s.gray[label] = true
		}
		c.y += h
		return label
	}
	gap := func(c *column, v int) { c.y += px(v) }
	button := func(c *column, x int32, t string, id int, def bool) int32 {
		w := max(px(124), s.lineWidth(t, body)+px(36))
		style := uint32(wsTabStop | bsPushButton)
		if def {
			style = wsTabStop | bsDefPushButton
		}
		s.child(0, "BUTTON", t, style, x, c.y, w, px(32), id, body)
		return w
	}
	field := func(c *column, caption string, id int) {
		label := px(150)
		s.child(0, "STATIC", caption, ssNoPrefix, c.x, c.y+px(6), label, px(22), 0, body)
		e := s.child(wsExClientEdge, "EDIT", "", wsTabStop|esPassword|esAutoHScroll, c.x+label, c.y, c.w-label, px(30), id, body)
		sendMessage(e, emLimitText, core.MaxPassphrase, 0)
		c.y += px(38)
	}

	top := column{px(24), px(settingsWidth - 48), px(86)}
	if a.status != "" {
		label := text(&top, a.status, body, false)
		if a.warning {
			s.warn = label
		}
		gap(&top, 16)
	}
	left := column{px(24), px(390), top.y}
	right := column{px(450), px(settingsWidth - 450 - 24), top.y}

	text(&left, "Lock", heading, false)
	gap(&left, 6)
	text(&left, "Blocks this PC's own keyboard, mouse, touchpad and touch screen. The screen stays on and your agents keep working.", body, false)
	gap(&left, 12)
	x := left.x + button(&left, left.x, "Lock Input", idLock, false) + px(10)
	button(&left, x, "15-Second Trial", idTrial, false)
	left.y += px(32)
	gap(&left, 10)
	shortcut := core.HotkeyLabel + " also locks."
	if !a.hotkey {
		shortcut = core.HotkeyLabel + " is taken by another app, so lock from the tray icon."
	}
	text(&left, shortcut+" While locked, press "+core.HotkeyLabel+" or click 5 times quickly to bring up the unlock prompt. New here? Try the trial first: it unlocks by itself after 15 seconds.", small, true)
	gap(&left, 22)
	text(&left, "If you're stuck", heading, false)
	gap(&left, 6)
	text(&left, "Ctrl+Alt+Del always works: choose Lock, then sign back in, and Input Guard is off. If Input Guard freezes, its recovery helper ends it within seconds.", small, true)
	if !a.elevated {
		gap(&left, 8)
		text(&left, "Not running as administrator, so windows opened as administrator, like Task Manager, can still get keyboard input while locked.", small, true)
	}
	gap(&left, 12)
	text(&left, "Closing this window keeps Input Guard in the notification area (click ^ by the clock). Quit it from there.", small, true)

	text(&right, "Passphrase", heading, false)
	gap(&right, 8)
	if _, err := os.Stat(a.vault.Path); err == nil {
		field(&right, "Current passphrase", idCurrent)
	}
	field(&right, "New passphrase", idNew)
	field(&right, "Type it again", idAgain)
	gap(&right, 2)
	w := button(&right, right.x, "Save Passphrase", idSave, true)
	s.note = s.child(0, "STATIC", s.saveNote, ssNoPrefix, right.x+w+px(12), right.y+px(7), right.w-w-px(12), px(22), 0, body)
	right.y += px(32)
	gap(&right, 10)
	text(&right, "4 to 128 characters typed on an English keyboard. Don't reuse your Windows password. Only a salted hash is kept, sealed for this Windows user.", small, true)
	gap(&right, 22)
	text(&right, "Agents and remote control", heading, false)
	gap(&right, 8)
	box := s.child(0, "BUTTON", "Allow automation events (experimental)", wsTabStop|bsAutoCheckbox, right.x, right.y, right.w, px(24), idAutomation, body)
	if a.options.AllowAutomation {
		sendMessage(box, bmSetCheck, 1, 0)
	}
	right.y += px(30)
	text(&right, "Lets through keys and clicks that Windows marks as sent by software: agents, and remote control that sends input that way. This PC's own keyboard and mouse stay blocked, but the touch screen does not, and 5 clicks don't open the prompt.", small, true)
	gap(&right, 12)
	button(&right, right.x, "Check Input Sources…", idCheck, false)
	right.y += px(32)
	gap(&right, 8)
	text(&right, "Counts for 10 seconds where keys and clicks come from, to show how Splashtop's input reaches this PC. Nothing is blocked; only counts are kept.", small, true)

	s.height = max(left.y, right.y) + px(24)
	w2, h2 := s.outerSize()
	setWindowPos(s.hwnd, 0, 0, 0, w2, h2, swpNoMove|swpNoZOrder|swpNoActivate)
	call(pInvalidateRect, s.hwnd, 0, 1)
}

func (s *settingsWindow) lineWidth(text string, font uintptr) int32 {
	dc := call(pGetDC, s.hwnd)
	defer call(pReleaseDC, s.hwnd, dc)
	old := call(pSelectObject, dc, font)
	r := rect{}
	call(pDrawTextW, dc, uintptr(wsz(text)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), dtCalcRect|dtSingleLine|dtNoPrefix)
	call(pSelectObject, dc, old)
	return r.Right
}

func (s *settingsWindow) field(id int) string {
	c := call(pGetDlgItem, s.hwnd, uintptr(id))
	if c == 0 {
		return ""
	}
	return windowText(c)
}

func (s *settingsWindow) setNote(text string) {
	s.saveNote = text
	if s.note != 0 {
		call(pSetWindowTextW, s.note, uintptr(wsz(text)))
	}
}

func (s *settingsWindow) clearFields() {
	for _, id := range []int{idCurrent, idNew, idAgain} {
		if c := call(pGetDlgItem, s.hwnd, uintptr(id)); c != 0 {
			call(pSetWindowTextW, c, uintptr(wsz("")))
		}
	}
}

// save checks the current passphrase and saves the new one off the UI
// thread, since each takes 600,000 rounds. The same rules as on the Mac.
func (s *settingsWindow) save() {
	if s.saving || a.locked {
		return
	}
	current, next, again := s.field(idCurrent), s.field(idNew), s.field(idAgain)
	switch {
	case next == "" && again == "":
		s.setNote("Type the new passphrase in both boxes.")
		return
	case next != again:
		s.setNote("The new passphrases do not match.")
		return
	case !core.ValidPassphrase(next):
		s.setNote("Use 4 to 128 characters from an English keyboard.")
		return
	}
	s.saving = true
	s.setNote("Saving…")
	call(pEnableWindow, call(pGetDlgItem, s.hwnd, idSave), 0)
	vault := a.vault
	go func() {
		result := saveResult{}
		existing, err := vault.Read()
		switch {
		case err != nil:
			result.message = err.Error() + " To start over, delete " + vault.Path + "."
		case existing != nil && !existing.Matches(current):
			result.message = "Enter your current Input Guard passphrase to change it."
		default:
			record, err := core.NewRecord(next)
			if err == nil {
				err = vault.Save(record)
			}
			switch {
			case err != nil:
				result.message = err.Error()
			case existing == nil:
				result = saveResult{true, "✓ Passphrase saved."}
			default:
				result = saveResult{true, "✓ Passphrase reset."}
			}
		}
		a.saves <- result
		postMessage(mainWindow, wmSaved, 0, 0)
	}()
}

func (s *settingsWindow) saved(result saveResult) {
	s.saving = false
	s.saveNote = result.message
	if s.hwnd == 0 {
		return
	}
	s.clearFields()
	if result.ok {
		a.status, a.warning = result.message[len("✓ "):]+" You can lock whenever you're ready.", false
		s.build()
		return
	}
	call(pEnableWindow, call(pGetDlgItem, s.hwnd, idSave), 1)
	s.setNote(result.message)
}

func settingsProc(hwnd, message, wParam, lParam uintptr) uintptr {
	s := &a.settings
	switch uint32(message) {
	case wmCommand:
		id, code := int(loword(wParam)), hiword(wParam)
		switch id {
		case idLock:
			a.beginLock(false)
		case idTrial:
			a.beginLock(true)
		case idSave:
			s.save()
		case idCancel:
			s.close()
		case idCheck:
			a.startInputCheck()
		case idAutomation:
			if code == bnClicked {
				a.options.AllowAutomation = uint32(sendMessage(lParam, bmGetCheck, 0, 0)) == 1
				if err := core.SaveSettings(a.settingsPath, a.options); err != nil {
					a.setStatus("Could not save the setting: "+err.Error(), true)
				}
			}
		}
		return 0
	case wmCtlColorStatic:
		hdc := wParam
		call(pSetBkColor, hdc, call(pGetSysColor, colorWindow))
		color := call(pGetSysColor, colorWindowText)
		if s.gray[lParam] {
			color = secondaryColor
		}
		if lParam == s.warn && s.warn != 0 {
			color = warningColor
		}
		call(pSetTextColor, hdc, color)
		return call(pGetSysColorBrush, colorWindow)
	case wmPaint:
		var ps paintStruct
		hdc := call(pBeginPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
		d := s.dpi
		mark := int(scale(48, d))
		call(pDrawIconEx, hdc, uintptr(scale(24, d)), uintptr(scale(24, d)), a.brand(mark), uintptr(mark), uintptr(mark), 0, 0, diNormal)
		call(pSetBkMode, hdc, 1)
		drawText(hdc, "Input Guard", s.fonts[0], call(pGetSysColor, colorWindowText),
			rect{scale(86, d), scale(24, d), scale(settingsWidth-24, d), scale(52, d)}, dtSingleLine)
		subtitle := "for Windows · version " + version + " · prototype"
		if a.elevated {
			subtitle += " · running as administrator"
		}
		drawText(hdc, subtitle, s.fonts[3], secondaryColor, rect{scale(86, d), scale(54, d), scale(settingsWidth-24, d), scale(76, d)}, dtSingleLine)
		call(pEndPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmDPIChanged:
		suggested := (*rect)(unsafe.Pointer(lParam))
		setWindowPos(hwnd, 0, suggested.Left, suggested.Top, suggested.Right-suggested.Left, suggested.Bottom-suggested.Top, swpNoZOrder|swpNoActivate)
		s.build()
		sendMessage(hwnd, wmSetIcon, 0, a.brand(int(scale(16, s.dpi))))
		sendMessage(hwnd, wmSetIcon, 1, a.brand(int(scale(32, s.dpi))))
		return 0
	case wmRebuild:
		if !s.saving {
			s.build()
		}
		return 0
	case wmClose:
		s.close()
		return 0
	case wmDestroy:
		s.hwnd, s.children, s.warn, s.note = 0, nil, 0, 0
		return 0
	}
	return call(pDefWindowProcW, hwnd, message, wParam, lParam)
}
