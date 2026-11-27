//go:build windows

package main

// The notice in the middle of the screen, the clear cover over every screen
// that catches touches, the icons, and the DPAPI seal for the passphrase file.

import (
	"errors"
	"image"
	"strings"
	"syscall"
	"unsafe"

	"inputguard/core"
)

var (
	noticeCallback = syscall.NewCallback(noticeProc)
	shieldCallback = syscall.NewCallback(shieldProc)

	pGetMessageExtraInfo = user32.NewProc("GetMessageExtraInfo")
	pValidateRect        = user32.NewProc("ValidateRect")
)

const noticeHideTimer = 1

// notice shows "Agent working", the trial's countdown, or the unlock prompt.
// It never takes the focus and clicks go through it, as on the Mac.
type notice struct {
	hwnd    uintptr
	visible bool
	light   bool
	dpi     uint32
	title   uintptr
	body    uintptr
	dots    uintptr
}

func (n *notice) ensure() bool {
	if n.hwnd == 0 {
		n.hwnd = createWindow(wsExTopmost|wsExToolWindow|wsExNoActivate|wsExLayered|wsExTransparent,
			"InputGuardNotice", "Input Guard", wsPopup, 0, 0, 1, 1, 0, 0)
		if n.hwnd == 0 {
			return false
		}
		call(pSetLayeredWindowAttributes, n.hwnd, 0, 248, lwaAlpha)
	}
	return true
}

// show puts up whichever notice applies now. The working notice goes away
// after 4 seconds; the trial and the prompt stay.
func (n *notice) show() {
	if !n.ensure() {
		return
	}
	n.light = lightTheme()
	n.place()
	n.visible = true
	call(pKillTimer, n.hwnd, noticeHideTimer)
	shared.Lock()
	entering := shared.guard.Entering()
	shared.Unlock()
	if !entering && a.trial == 0 {
		call(pSetTimer, n.hwnd, noticeHideTimer, 4000, 0)
	}
	call(pInvalidateRect, n.hwnd, 0, 0)
}

func (n *notice) place() {
	var mi monitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	monitor := call(pMonitorFromPoint, 0, monitorDefaultToPrimary) // the point (0, 0)
	call(pGetMonitorInfoW, monitor, uintptr(unsafe.Pointer(&mi)))
	// Twice: once to reach the main screen, once more at that screen's scale.
	for range 2 {
		dpi := dpiOf(n.hwnd)
		if dpi != n.dpi || n.title == 0 {
			for _, f := range []uintptr{n.title, n.body, n.dots} {
				if f != 0 {
					call(pDeleteObject, f)
				}
			}
			n.dpi = dpi
			n.title = createFont(17, 600, dpi)
			n.body = createFont(11, 400, dpi)
			n.dots = createFont(14, 600, dpi)
		}
		w, h := scale(480, dpi), scale(200, dpi)
		x := mi.Work.Left + (mi.Work.Right-mi.Work.Left-w)/2
		y := mi.Work.Top + (mi.Work.Bottom-mi.Work.Top-h)/2
		setWindowPos(n.hwnd, hwndTopmost, x, y, w, h, swpNoActivate|swpShowWindow)
	}
	w, h := scale(480, n.dpi), scale(200, n.dpi)
	corner := scale(32, n.dpi) // the ellipse's size: a 16-pixel radius
	call(pSetWindowRgn, n.hwnd, call(pCreateRoundRectRgn, 0, 0, uintptr(w+1), uintptr(h+1), uintptr(corner), uintptr(corner)), 1)
}

func (n *notice) hide() {
	if n.hwnd != 0 {
		call(pKillTimer, n.hwnd, noticeHideTimer)
		call(pShowWindow, n.hwnd, swHide)
	}
	n.visible = false
}

func (n *notice) redraw() {
	if n.visible {
		call(pInvalidateRect, n.hwnd, 0, 0)
	}
}

// tick keeps the notice in front, and the trial's countdown and the click
// dots current.
func (n *notice) tick() {
	if !n.visible {
		return
	}
	setWindowPos(n.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	n.redraw()
}

func (n *notice) content() (title, text, dots string) {
	shared.Lock()
	entering, note, typed, automation := shared.guard.Entering(), shared.guard.Note(), shared.guard.Typed(), shared.guard.Automation()
	clicks := shared.guard.Clicks(core.SystemClock.Now())
	shared.Unlock()
	if clicks > 0 {
		// Clicks so far, out of five, since the cursor doesn't move while locked.
		dots = strings.Repeat("●", clicks) + strings.Repeat("○", core.UnlockClicks-clicks)
	}
	switch {
	case entering:
		return "Resume input", note, strings.Repeat("•", min(typed, 36))
	case a.trial > 0:
		text = "TEST MODE · input comes back by itself in " + core.Seconds(a.trial-core.SystemClock.Now()) + "."
		if automation {
			return "Input test", text + "\nTry the keyboard and mouse now.", dots
		}
		return "Input test", text + "\nTry the keyboard, mouse and touch screen now.\nFive quick clicks open the unlock prompt.", dots
	case automation:
		return "Agent working", "Press " + core.HotkeyLabel + " when you're ready to resume input.", ""
	default:
		return "Agent working", "Press " + core.HotkeyLabel + " or click 5 times when you're ready to resume input.", dots
	}
}

func drawText(hdc uintptr, text string, font uintptr, color uintptr, r rect, flags uint32) {
	call(pSelectObject, hdc, font)
	call(pSetTextColor, hdc, color)
	text = strings.ReplaceAll(text, "\n", "\r\n")
	call(pDrawTextW, hdc, uintptr(wsz(text)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), uintptr(flags|dtNoPrefix))
}

func noticeProc(hwnd, message, wParam, lParam uintptr) uintptr {
	n := &a.notice
	switch uint32(message) {
	case wmEraseBkgnd:
		return 1 // painted in full below, without flicker
	case wmTimer:
		if wParam == noticeHideTimer {
			shared.Lock()
			entering := shared.guard.Entering()
			shared.Unlock()
			if !entering {
				n.hide()
			}
		}
		return 0
	case wmPaint:
		var ps paintStruct
		hdc := call(pBeginPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
		var r rect
		call(pGetClientRect, hwnd, uintptr(unsafe.Pointer(&r)))
		background, primary, secondary := rgb(250, 250, 249), rgb(26, 26, 26), rgb(70, 70, 70)
		if !n.light {
			background, primary, secondary = rgb(36, 38, 38), rgb(242, 242, 242), rgb(205, 205, 205)
		}
		brush := call(pCreateSolidBrush, background)
		call(pFillRect, hdc, uintptr(unsafe.Pointer(&r)), brush)
		call(pDeleteObject, brush)
		d := n.dpi
		mark := int(scale(40, d))
		call(pDrawIconEx, hdc, uintptr(scale(24, d)), uintptr(scale(24, d)), a.brand(mark), uintptr(mark), uintptr(mark), 0, 0, diNormal)
		call(pSetBkMode, hdc, 1) // transparent
		title, text, dots := n.content()
		drawText(hdc, title, n.title, primary, rect{scale(78, d), scale(28, d), r.Right - scale(24, d), scale(62, d)}, dtSingleLine)
		drawText(hdc, text, n.body, secondary, rect{scale(24, d), scale(78, d), r.Right - scale(24, d), r.Bottom - scale(44, d)}, dtWordBreak)
		drawText(hdc, dots, n.dots, primary, rect{scale(24, d), r.Bottom - scale(44, d), r.Right - scale(24, d), r.Bottom - scale(12, d)}, dtSingleLine)
		call(pEndPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	}
	return call(pDefWindowProcW, hwnd, message, wParam, lParam)
}

// shield is one nearly clear window, at 1/255 opacity, over every screen
// and above other windows. Touches go to the window under the finger, never
// through the hooks, so they land here and do nothing. It never takes the
// focus, so the agent's app stays in front. When Input Guard ends for any
// reason, Windows removes it with the process.
type shield struct {
	hwnd   uintptr
	raised bool
}

func (s *shield) raise() bool {
	if s.hwnd == 0 {
		s.hwnd = createWindow(wsExTopmost|wsExToolWindow|wsExNoActivate|wsExLayered, "InputGuardShield", "", wsPopup, 0, 0, 1, 1, 0, 0)
		if s.hwnd == 0 {
			return false
		}
		call(pSetLayeredWindowAttributes, s.hwnd, 0, 1, lwaAlpha) // not 0: a fully clear window lets touches through
	}
	x := int32(call(pGetSystemMetrics, smXVirtualScreen))
	y := int32(call(pGetSystemMetrics, smYVirtualScreen))
	w := int32(call(pGetSystemMetrics, smCXVirtualScreen))
	h := int32(call(pGetSystemMetrics, smCYVirtualScreen))
	setWindowPos(s.hwnd, hwndTopmost, x, y, w, h, swpNoActivate|swpShowWindow)
	s.raised = true
	return true
}

func (s *shield) lower() {
	if s.hwnd != 0 {
		call(pShowWindow, s.hwnd, swHide)
	}
	s.raised = false
}

// shieldTap counts a touch toward the five that open the prompt.
func shieldTap() {
	shared.Lock()
	e := shared.guard.Touch(core.SystemClock.Now())
	shared.Unlock()
	if e.ShowPrompt || e.Counted {
		a.notice.show()
	}
}

func shieldProc(hwnd, message, wParam, lParam uintptr) uintptr {
	m := uint32(message)
	switch m {
	case wmMouseActivate:
		return maNoActivate
	case wmPointerActivate:
		return paNoActivate
	case wmPointerDown:
		var kind uint32
		if uint32(call(pGetPointerType, uintptr(loword(wParam)), uintptr(unsafe.Pointer(&kind)))) != 0 && (kind == ptTouch || kind == ptPen) {
			shieldTap()
		}
		return 0 // handled, so Windows doesn't turn it into a click
	case wmPointerUpdate, wmPointerUp, 0x0249, 0x024A, 0x024E, 0x024F: // enter, leave, wheels
		return 0
	case wmLButtonDown, wmRButtonDown, wmMButtonDown, wmXButtonDown:
		// Clicks Windows made from a touch or pen carry its tag and were
		// counted as taps above. Any other click reaching the cover got past
		// the mouse hook. Running as administrator, the hook sees every
		// click, so this means Windows dropped it: end the lock and say so,
		// as the Mac app does. Without administrator rights, it can also be a
		// click while an administrator window is in front: it's absorbed,
		// and a left click counts toward the five.
		if uint32(call(pGetMessageExtraInfo))&touchSignatureMask != touchSignature {
			if a.elevated {
				a.hookLost()
			} else if m == wmLButtonDown {
				shieldTap()
			}
		}
		return 0
	case wmPaint:
		call(pValidateRect, hwnd, 0)
		return 0
	}
	if m >= wmMouseFirst && m <= wmMouseLast {
		return 0
	}
	return call(pDefWindowProcW, hwnd, message, wParam, lParam)
}

// makeIcon turns a drawn image into a Windows icon with its transparency.
func makeIcon(img *image.NRGBA) uintptr {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	header := bitmapInfoHeader{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	color := call(pCreateDIBSection, 0, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color == 0 || bits == nil {
		return 0
	}
	pixels := unsafe.Slice((*byte)(bits), w*h*4)
	stride := (w + 15) / 16 * 2 // monochrome rows are whole 16-bit words
	mask := make([]byte, stride*h)
	for y := range h {
		for x := range w {
			c := img.NRGBAAt(x, y)
			i := (y*w + x) * 4
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = c.B, c.G, c.R, c.A
			if c.A == 0 {
				mask[y*stride+x/8] |= 0x80 >> (x % 8)
			}
		}
	}
	maskBitmap := call(pCreateBitmap, uintptr(w), uintptr(h), 1, 1, uintptr(unsafe.Pointer(&mask[0])))
	info := iconInfo{Icon: 1, Mask: maskBitmap, Color: color}
	icon := call(pCreateIconIndirect, uintptr(unsafe.Pointer(&info)))
	call(pDeleteObject, color)
	call(pDeleteObject, maskBitmap)
	return icon
}

// dpapi seals the verifier with Windows' Data Protection API for the current
// user: only this Windows account on this PC can unseal it, and a changed
// file fails to unseal.
type dpapi struct{}

var dpapiEntropy = []byte("Input Guard passphrase verifier")

func (dpapi) Protect(plain []byte) ([]byte, error) { return dpapiCall(pCryptProtectData, plain, true) }
func (dpapi) Unprotect(sealed []byte) ([]byte, error) {
	return dpapiCall(pCryptUnprotectData, sealed, false)
}

func dpapiCall(p *syscall.LazyProc, data []byte, protect bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("nothing to seal")
	}
	in := dataBlob{uint32(len(data)), &data[0]}
	entropy := dataBlob{uint32(len(dpapiEntropy)), &dpapiEntropy[0]}
	var out dataBlob
	var description *uint16 // only when sealing; unsealing doesn't ask for it back
	if protect {
		description = wstr("Input Guard passphrase verifier")
	}
	r, _, err := p.Call(uintptr(unsafe.Pointer(&in)), uintptr(unsafe.Pointer(description)), uintptr(unsafe.Pointer(&entropy)), 0, 0,
		cryptProtectUIForbidden, uintptr(unsafe.Pointer(&out)))
	if uint32(r) == 0 || out.Data == nil {
		return nil, err
	}
	sealed := unsafe.Slice(out.Data, out.Size)
	result := append([]byte(nil), sealed...)
	clear(sealed)
	call(pLocalFree, uintptr(unsafe.Pointer(out.Data)))
	return result, nil
}
