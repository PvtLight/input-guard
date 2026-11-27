package core

import "time"

// KeyEvent is what the low-level keyboard hook sees, without Windows types.
type KeyEvent struct {
	VK       uint32
	Scan     uint32 // for VKPacket, the character
	Up       bool
	Injected bool // LLKHF_INJECTED: sent by software (SendInput and the like)
}

type MouseKind int

const (
	MouseMove MouseKind = iota
	MouseDown
	MouseUp
	MouseWheel
)

// MouseEvent is what the low-level mouse hook sees.
type MouseEvent struct {
	Kind     MouseKind
	Button   uint32 // VKLButton, VKRButton, VKMButton, VKXButton1 or VKXButton2
	Injected bool   // LLMHF_INJECTED
	Touch    bool   // a click Windows made from a touch or pen; the touch shield counts those
}

// Effects tells the hook what to do with an event, and the app what to show.
type Effects struct {
	Pass       bool // let the event through to apps
	ShowPrompt bool // the unlock prompt just opened
	HidePrompt bool // Esc put the prompt away; still locked
	Verify     bool // a passphrase is ready: call TakeSubmission
	Redraw     bool // the prompt's message or dots changed
	Counted    bool // a click counted toward the five; show how many so far
}

// Guard holds the lock and decides, for every key and mouse event, whether it
// reaches apps. The hooks call it for each event, and the tray menu, the
// shortcut and the touch shield call it too. It has no Windows types, so the
// replays in guard_test.go run the same rules the hooks use. Callers serialize
// access; the app uses one mutex.
//
// While locked, every key press and click is blocked. What passes:
//   - releases of keys and buttons that apps already saw pressed, such as the
//     Ctrl+Alt+Shift still held when the shortcut starts the lock; without
//     them apps would keep treating those keys as held after unlock;
//   - with automation allowed, events Windows marks as injected by software,
//     except while the unlock prompt is open.
type Guard struct {
	locked     bool
	automation bool
	entering   bool
	verifying  bool
	session    uint64
	prompt     UnlockPrompt
	limiter    AttemptLimiter
	clicks     ClickUnlock
	opened     bool
	lastPrompt time.Duration
	pending    string
	held       [256]bool // keys and buttons down, as the hooks see them
	appsSee    [256]bool // keys and buttons apps saw go down and not yet up
	capsLock   bool      // Caps Lock as apps see it
	promptCaps bool      // Caps Lock as the prompt types with it
}

// Lock starts guarding. down lists the keys and mouse buttons down right now
// as apps see them (the app reads GetAsyncKeyState after its hooks are in
// place), so their releases still pass. capsLock is Caps Lock as apps see it.
func (g *Guard) Lock(down []uint32, capsLock, allowAutomation bool) uint64 {
	session := g.session + 1
	g.prompt.Clear()
	*g = Guard{locked: true, automation: allowAutomation, capsLock: capsLock, session: session}
	for _, vk := range down {
		if vk = canonical(vk); vk < 256 {
			g.held[vk] = true
			g.appsSee[vk] = true
		}
	}
	return session
}

// Unlock ends guarding. Replies from a passphrase check started before are
// ignored after this.
func (g *Guard) Unlock() {
	session := g.session + 1
	g.prompt.Clear()
	*g = Guard{session: session}
}

func (g *Guard) Locked() bool     { return g.locked }
func (g *Guard) Entering() bool   { return g.entering }
func (g *Guard) Verifying() bool  { return g.verifying }
func (g *Guard) Automation() bool { return g.automation }
func (g *Guard) Session() uint64  { return g.session }
func (g *Guard) Note() string     { return g.prompt.Note() }
func (g *Guard) Typed() int       { return g.prompt.Length() }

func (g *Guard) down(left, right uint32) bool { return g.held[left] || g.held[right] }
func (g *Guard) ctrl() bool                   { return g.down(VKLControl, VKRControl) }
func (g *Guard) alt() bool                    { return g.down(VKLMenu, VKRMenu) }
func (g *Guard) shift() bool                  { return g.down(VKLShift, VKRShift) }
func (g *Guard) win() bool                    { return g.down(VKLWin, VKRWin) }

// Key decides one keyboard event.
func (g *Guard) Key(ev KeyEvent, now time.Duration) (e Effects) {
	if !g.locked {
		e.Pass = true
		return
	}
	vk := canonical(ev.VK)
	if vk >= 256 {
		return
	}
	g.held[vk] = !ev.Up
	automated := g.automation && ev.Injected && !g.entering
	if ev.Up {
		if automated || g.appsSee[vk] {
			g.appsSee[vk] = false
			e.Pass = true
		}
		return
	}
	// The shortcut always opens the prompt and never reaches apps, whoever sent it.
	if vk == HotkeyVK && g.ctrl() && g.alt() && g.shift() && !g.win() {
		e.ShowPrompt = g.openPrompt(now)
		return
	}
	if automated {
		g.appsSee[vk] = true
		if vk == VKCapital {
			g.capsLock = !g.capsLock
		}
		e.Pass = true
		return
	}
	if !g.entering || g.verifying {
		return
	}
	switch vk {
	case VKEscape:
		g.ClosePrompt()
		e.HidePrompt = true
	case VKReturn:
		if submitted, ok := g.prompt.Submit(g.limiter, now); ok {
			g.pending = submitted
			g.verifying = true
			e.Verify = true
		}
		e.Redraw = true
	case VKBack:
		g.prompt.DeleteLast()
		e.Redraw = true
	case VKCapital:
		// The key stays blocked, so Caps Lock doesn't change for apps; the
		// prompt keeps its own, starting from the keyboard's.
		g.promptCaps = !g.promptCaps
		g.prompt.CapsLock = g.promptCaps
		e.Redraw = true
	default:
		if g.ctrl() || g.alt() || g.win() {
			return
		}
		before := g.prompt.Length()
		if vk == VKPacket {
			g.prompt.Type(string(rune(ev.Scan & 0xFFFF)))
		} else if c := TypedChar(vk, g.shift(), g.promptCaps); c != 0 {
			g.prompt.Type(string(c))
		}
		e.Redraw = g.prompt.Length() != before
	}
	return
}

// Mouse decides one mouse event. Clicks it blocks count toward the five that
// open the prompt.
func (g *Guard) Mouse(ev MouseEvent, now time.Duration) (e Effects) {
	if !g.locked {
		e.Pass = true
		return
	}
	automated := g.automation && ev.Injected && !g.entering
	b := ev.Button & 0xFF
	switch ev.Kind {
	case MouseUp:
		g.held[b] = false
		if automated || g.appsSee[b] {
			g.appsSee[b] = false
			e.Pass = true
		}
	case MouseDown:
		g.held[b] = true
		if automated {
			g.appsSee[b] = true
			e.Pass = true
		} else if b == VKLButton && !ev.Touch {
			e.ShowPrompt, e.Counted = g.click(now)
		}
	default:
		e.Pass = automated
	}
	return
}

// Touch counts a tap the touch shield absorbed.
func (g *Guard) Touch(now time.Duration) (e Effects) {
	if g.locked {
		e.ShowPrompt, e.Counted = g.click(now)
	}
	return
}

// click counts one click: opened is true on the fifth within three seconds,
// counted on the ones before it.
func (g *Guard) click(now time.Duration) (opened, counted bool) {
	if !ClicksCount(g.automation, g.entering) {
		return false, false
	}
	if !g.clicks.Click(now) {
		return false, true
	}
	return g.OpenPrompt(now), false
}

// Clicks is how many of the five are counted right now. Over remote control
// the cursor doesn't move while locked, so the notice shows these.
func (g *Guard) Clicks(now time.Duration) int {
	if !g.locked || !ClicksCount(g.automation, g.entering) {
		return 0
	}
	return g.clicks.Pending(now)
}

// OpenPrompt opens the unlock prompt from clicks, taps or the tray menu. True
// when it opened; it stays closed for a second after the last one opened.
func (g *Guard) OpenPrompt(now time.Duration) bool {
	if !g.openPrompt(now) {
		return false
	}
	// Nobody is holding the shortcut now, so drop any key the hooks missed
	// coming up, which would otherwise stop typing.
	g.ResetHeld()
	return true
}

func (g *Guard) openPrompt(now time.Duration) bool {
	if !g.locked || g.entering || (g.opened && now-g.lastPrompt <= time.Second) {
		return false
	}
	g.opened = true
	g.lastPrompt = now
	g.entering = true
	g.promptCaps = g.capsLock
	g.prompt.Clear()
	g.prompt = NewUnlockPrompt(g.promptCaps)
	return true
}

// ClosePrompt puts the prompt away and stays locked.
func (g *Guard) ClosePrompt() {
	g.entering = false
	g.verifying = false
	g.pending = ""
	g.prompt.Clear()
	g.prompt = NewUnlockPrompt(false)
}

// ResetHeld forgets which keys and buttons are down. The app calls it when
// Windows comes back from its own secure screen (Ctrl+Alt+Del, a UAC prompt):
// releases made there never reach the hooks, and a Ctrl or Alt that looks
// held would stop the prompt from typing.
func (g *Guard) ResetHeld() { g.held = [256]bool{} }

// TakeSubmission hands the passphrase from Enter to the verifier, with the
// session it belongs to.
func (g *Guard) TakeSubmission() (string, uint64) {
	submitted := g.pending
	g.pending = ""
	return submitted, g.session
}

// Verified takes the verifier's answer. True means unlock now. A late answer
// for an earlier session is ignored.
func (g *Guard) Verified(session uint64, matched bool, now time.Duration) bool {
	if !g.locked || session != g.session || !g.verifying {
		return false
	}
	g.verifying = false
	if matched {
		return true
	}
	g.prompt.Rejected(&g.limiter, now)
	return false
}
