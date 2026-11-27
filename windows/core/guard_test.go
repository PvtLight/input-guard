package core

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// replay drives a Guard the way Windows drives the hooks. Apps only learn
// about keys from the events that pass, so apps is what they believe is held;
// after every sequence it must agree with the keys really held.
type replay struct {
	g        Guard
	now      time.Duration
	down     map[uint32]bool // keys and buttons really held
	apps     map[uint32]bool // what apps believe is held
	leaked   []string        // presses that reached apps while they shouldn't
	prompts  int
	verifies []string
}

func newReplay() *replay {
	return &replay{down: map[uint32]bool{}, apps: map[uint32]bool{}}
}

func (r *replay) key(vk uint32, up, injected bool) Effects {
	return r.keyEvent(KeyEvent{VK: vk, Up: up, Injected: injected})
}

func (r *replay) keyEvent(ev KeyEvent) Effects {
	r.now += 10 * time.Millisecond
	c := canonical(ev.VK)
	if ev.Up {
		delete(r.down, c)
	} else {
		r.down[c] = true
	}
	allowed := !r.g.Locked() || (r.g.Automation() && ev.Injected && !r.g.Entering())
	e := r.g.Key(ev, r.now)
	r.effects(e)
	if e.Pass {
		if ev.Up {
			delete(r.apps, c)
		} else {
			if !allowed {
				r.leaked = append(r.leaked, fmt.Sprintf("key %#x", ev.VK))
			}
			r.apps[c] = true
		}
	}
	return e
}

func (r *replay) mouse(kind MouseKind, button uint32, injected, touch bool) Effects {
	r.now += 10 * time.Millisecond
	switch kind {
	case MouseDown:
		r.down[button] = true
	case MouseUp:
		delete(r.down, button)
	}
	allowed := !r.g.Locked() || (r.g.Automation() && injected && !r.g.Entering())
	e := r.g.Mouse(MouseEvent{Kind: kind, Button: button, Injected: injected, Touch: touch}, r.now)
	r.effects(e)
	if e.Pass {
		switch kind {
		case MouseDown:
			r.apps[button] = true
			if !allowed {
				r.leaked = append(r.leaked, fmt.Sprintf("button %#x", button))
			}
		case MouseUp:
			delete(r.apps, button)
		default:
			if !allowed {
				r.leaked = append(r.leaked, "pointer move or wheel")
			}
		}
	}
	return e
}

func (r *replay) effects(e Effects) {
	if e.ShowPrompt {
		r.prompts++
	}
	if e.Verify {
		submitted, _ := r.g.TakeSubmission()
		r.verifies = append(r.verifies, submitted)
	}
}

func (r *replay) press(vks ...uint32) {
	for _, vk := range vks {
		r.key(vk, false, false)
	}
}

func (r *replay) release(vks ...uint32) {
	for _, vk := range vks {
		r.key(vk, true, false)
	}
}

func (r *replay) tap(vks ...uint32) {
	for _, vk := range vks {
		r.key(vk, false, false)
		r.key(vk, true, false)
	}
}

// lock is what the app does: hooks first, then the keys apps see as held
// (GetAsyncKeyState), then the guard takes over.
func (r *replay) lock(automation bool) {
	var held []uint32
	for vk := range r.apps {
		held = append(held, vk)
	}
	r.g.Lock(held, false, automation)
}

func (r *replay) clicks(n int, gap time.Duration) {
	for range n {
		r.now += gap
		r.mouse(MouseDown, VKLButton, false, false)
		r.mouse(MouseUp, VKLButton, false, false)
	}
}

func orders(keys []uint32) [][]uint32 {
	if len(keys) <= 1 {
		return [][]uint32{slices.Clone(keys)}
	}
	var all [][]uint32
	for i := range keys {
		rest := append(slices.Clone(keys[:i]), keys[i+1:]...)
		for _, tail := range orders(rest) {
			all = append(all, append([]uint32{keys[i]}, tail...))
		}
	}
	return all
}

var hotkeyMods = []uint32{VKLControl, VKLMenu, VKLShift}

// Every press and release order of Ctrl+Alt+Shift+L, with the lock starting
// at every point while the shortcut is released, then the shortcut again to
// open the prompt in every order, then a passphrase typed with Shift.
func TestShortcutOrdersLeaveNothingHeld(t *testing.T) {
	runs, failures := 0, 0
	for _, pressLock := range orders(hotkeyMods) {
		for _, releaseLock := range orders([]uint32{HotkeyVK, VKLControl, VKLMenu, VKLShift}) {
			for lockAt := 0; lockAt <= 4; lockAt++ {
				for _, pressUnlock := range orders(hotkeyMods) {
					for _, releaseUnlock := range orders(hotkeyMods) {
						r := newReplay()
						r.press(pressLock...)
						r.press(HotkeyVK) // Windows sends the app WM_HOTKEY; the lock starts
						for i, vk := range releaseLock {
							if i == lockAt {
								r.lock(false)
							}
							r.release(vk)
						}
						if lockAt == 4 {
							r.lock(false)
						}
						r.press(pressUnlock...)
						opened := r.key(HotkeyVK, false, false).ShowPrompt
						r.release(HotkeyVK)
						r.release(releaseUnlock...)
						r.press(VKLShift)
						r.tap('O')
						r.release(VKLShift)
						r.tap('K', VKSpace, 'G', 'O')
						verify := r.key(VKReturn, false, false).Verify
						unlocked := r.g.Verified(r.g.Session(), true, r.now)
						r.g.Unlock()
						r.release(VKReturn)
						runs++
						if !opened || !verify || !unlocked || r.prompts != 1 || len(r.verifies) != 1 || r.verifies[0] != "Ok go" ||
							len(r.apps) != 0 || len(r.leaked) != 0 {
							failures++
							if failures < 4 {
								t.Errorf("press %x release %x lock at %d, unlock %x/%x: opened %v verify %v %q apps %v leaked %v",
									pressLock, releaseLock, lockAt, pressUnlock, releaseUnlock, opened, verify, r.verifies, r.apps, r.leaked)
							}
						}
					}
				}
			}
		}
	}
	if failures > 0 {
		t.Fatalf("%d of %d lock/unlock key orders failed", failures, runs)
	}
	t.Logf("%d lock/unlock key orders: the shortcut opens the prompt, the passphrase types, nothing reaches apps, nothing stays held", runs)
}

func TestShortcutsAndPlainKeysStayBlocked(t *testing.T) {
	shortcuts := []struct {
		name string
		mods []uint32
		key  uint32
	}{
		{"Alt+Tab", []uint32{VKLMenu}, VKTab},
		{"Alt+F4", []uint32{VKLMenu}, VKF4},
		{"Win (Start menu)", nil, VKLWin},
		{"Win+D", []uint32{VKLWin}, 'D'},
		{"Win+R", []uint32{VKRWin}, 'R'},
		{"Ctrl+Esc", []uint32{VKLControl}, VKEscape},
		{"Ctrl+Shift+Esc", []uint32{VKLControl, VKLShift}, VKEscape},
		{"Ctrl+C", []uint32{VKRControl}, 'C'},
		{"Ctrl+Alt+L", []uint32{VKLControl, VKLMenu}, 'L'},
		{"Ctrl+Alt+Shift+Win+L", []uint32{VKLControl, VKLMenu, VKLShift, VKLWin}, 'L'},
		{"plain A", nil, 'A'},
		{"Enter", nil, VKReturn},
		{"Shift+letters", []uint32{VKRShift}, 'Q'},
		{"generic Shift sent by software", []uint32{VKShift}, 'Z'},
	}
	for _, c := range shortcuts {
		r := newReplay()
		r.lock(false)
		r.press(c.mods...)
		r.tap(c.key)
		slices.Reverse(c.mods)
		r.release(c.mods...)
		if len(r.leaked) != 0 || len(r.apps) != 0 || r.prompts != 0 {
			t.Errorf("%s: leaked %v, apps hold %v, prompts %d", c.name, r.leaked, r.apps, r.prompts)
		}
	}
}

func TestAutomationEvents(t *testing.T) {
	r := newReplay()
	r.lock(true)
	if !r.key(VKLControl, false, true).Pass || !r.key('C', false, true).Pass || !r.key('C', true, true).Pass || !r.key(VKLControl, true, true).Pass {
		t.Error("injected keys pass with automation allowed")
	}
	if r.key('X', false, false).Pass || r.mouse(MouseMove, 0, false, false).Pass || r.mouse(MouseDown, VKLButton, false, false).Pass {
		t.Error("the PC's own keys and mouse stay blocked with automation allowed")
	}
	if !r.mouse(MouseMove, 0, true, false).Pass || !r.mouse(MouseDown, VKLButton, true, false).Pass || !r.mouse(MouseUp, VKLButton, true, false).Pass {
		t.Error("injected pointer moves and clicks pass with automation allowed")
	}
	// An agent holds Shift, then someone opens the prompt (the tray menu's
	// Unlock… over remote control): injected input stops until it closes,
	// but the agent's Shift release still passes, so Shift isn't stuck.
	r.key(VKLShift, false, true)
	r.mouse(MouseDown, VKRButton, true, false)
	if !r.g.OpenPrompt(r.now) {
		t.Fatal("the prompt opens from the menu")
	}
	if r.key('A', false, true).Pass || r.mouse(MouseMove, 0, true, false).Pass || r.mouse(MouseDown, VKLButton, true, false).Pass {
		t.Error("injected input is held back while the prompt is open")
	}
	r.key('A', true, true)
	r.mouse(MouseUp, VKLButton, true, false)
	if !r.key(VKLShift, true, true).Pass || !r.mouse(MouseUp, VKRButton, true, false).Pass {
		t.Error("releases of injected keys and buttons apps saw pressed pass while the prompt is open")
	}
	r.key(VKEscape, false, false)
	r.key(VKEscape, true, false)
	if !r.key('B', false, true).Pass {
		t.Error("injected keys pass again once the prompt is put away")
	}
	r.key('B', true, true)
	// The shortcut from software opens the prompt too, and never reaches apps.
	r.now += 2 * time.Second
	r.key(VKLControl, false, true)
	r.key(VKLMenu, false, true)
	r.key(VKLShift, false, true)
	if e := r.key(HotkeyVK, false, true); e.Pass || !e.ShowPrompt {
		t.Errorf("an injected shortcut opens the prompt and is blocked: %+v", e)
	}
	r.key(HotkeyVK, true, true)
	r.key(VKLShift, true, true)
	r.key(VKLMenu, true, true)
	r.key(VKLControl, true, true)
	r.clicks(5, 100*time.Millisecond)
	if len(r.apps) != 0 || len(r.leaked) != 0 {
		t.Errorf("apps hold %v, leaked %v", r.apps, r.leaked)
	}
	clicker := newReplay()
	clicker.lock(true)
	clicker.clicks(5, 100*time.Millisecond)
	if clicker.prompts != 0 {
		t.Error("five clicks don't open the prompt with automation allowed")
	}
}

func TestPromptTyping(t *testing.T) {
	r := newReplay()
	r.g.Lock(nil, true, false) // Caps Lock on for apps
	r.clicks(5, 50*time.Millisecond)
	if r.prompts != 1 || !r.g.Entering() || r.g.Note() != PromptInstructions+"\nCaps Lock is on." {
		t.Fatalf("five clicks open the prompt, which says Caps Lock is on: %q", r.g.Note())
	}
	r.tap('A', 'B')       // "AB" with Caps Lock
	r.tap(VKCapital)      // off, for the prompt only
	r.press(VKLShift)     //
	r.tap('C', '1', 0xBF) // "C!?"
	r.release(VKLShift)
	r.tap('D', VKNumpad0+7, VKSubtract, VKSpace, 0xDE) // "d7- '"
	r.keyEvent(KeyEvent{VK: VKPacket, Scan: 'x'})      // a character sent as Unicode
	r.keyEvent(KeyEvent{VK: VKPacket, Scan: 'x', Up: true})
	r.keyEvent(KeyEvent{VK: VKPacket, Scan: 0x00E9}) // é is not ASCII
	r.press(VKLControl)
	r.tap('V') // Ctrl+V types nothing
	r.release(VKLControl)
	r.press(VKRMenu)
	r.tap('Q') // nor does AltGr
	r.release(VKRMenu)
	r.tap('E', VKBack)
	if got := r.g.prompt.Candidate(); got != "ABC!?d7- 'x" {
		t.Errorf("typed %q", got)
	}
	if r.g.Note() != PromptInstructions || r.g.Typed() != 11 {
		t.Errorf("Caps Lock is off in the note after pressing it: %q", r.g.Note())
	}
	if len(r.leaked) != 0 || len(r.apps) != 0 {
		t.Errorf("typing the passphrase reaches no app: %v %v", r.leaked, r.apps)
	}
	if !r.g.capsLock {
		t.Error("Caps Lock for apps is unchanged by the prompt")
	}
	// The shortcut while the prompt is open types nothing.
	r.press(VKLControl, VKLMenu, VKLShift)
	r.tap(HotkeyVK)
	r.release(VKLShift, VKLMenu, VKLControl)
	if r.g.Typed() != 11 || r.prompts != 1 {
		t.Error("the shortcut neither types nor reopens the prompt")
	}

	empty := newReplay()
	empty.lock(false)
	empty.g.OpenPrompt(empty.now)
	if e := empty.key(VKReturn, false, false); e.Verify || empty.g.limiter.Failures != 0 {
		t.Error("Enter with nothing typed is not checked")
	}
}

func TestPromptOpenCloseAndVerify(t *testing.T) {
	r := newReplay()
	r.lock(false)
	if !r.g.OpenPrompt(r.now) {
		t.Fatal("the prompt opens")
	}
	r.tap('W', 'R', 'O', 'N', 'G')
	session := r.g.Session()
	if !r.key(VKReturn, false, false).Verify || !r.g.Verifying() {
		t.Fatal("Enter hands the passphrase to the verifier")
	}
	r.tap('Z') // typing while it checks is ignored
	if r.g.Verified(session+1, true, r.now) {
		t.Error("an answer for another session is ignored")
	}
	if r.g.Verified(session, false, r.now) || r.g.Note() != "That passphrase did not match. Wait 2 seconds, then try again." {
		t.Errorf("a mismatch shows the wait: %q", r.g.Note())
	}
	r.tap('R', 'I', 'G', 'H', 'T')
	if r.key(VKReturn, false, false).Verify || r.g.prompt.Candidate() != "right" {
		t.Error("Enter during the wait keeps what was typed")
	}
	r.now += 2 * time.Second
	if !r.key(VKReturn, false, false).Verify || r.verifies[1] != "right" || !r.g.Verified(session, true, r.now) {
		t.Error("Enter after the wait checks it, and a match unlocks")
	}

	r = newReplay()
	r.lock(false)
	r.g.OpenPrompt(r.now)
	r.tap('A')
	if !r.key(VKEscape, false, false).HidePrompt || r.g.Entering() || !r.g.Locked() || r.g.Typed() != 0 {
		t.Error("Esc puts the prompt away, forgets the typing, and stays locked")
	}
	r.release(VKEscape)
	if r.g.OpenPrompt(r.now) {
		t.Error("the prompt waits a second before opening again")
	}
	r.now += 1100 * time.Millisecond
	if !r.g.OpenPrompt(r.now) {
		t.Error("then it opens")
	}
	stale := r.g.Session()
	r.g.Unlock()
	r.g.Lock(nil, false, false)
	if r.g.Verified(stale, true, r.now) {
		t.Error("an answer from before an unlock never unlocks a later lock")
	}
}

// Ctrl+Alt+Del: Windows takes over before the release of Ctrl and Alt reaches
// the hooks. Back from that screen, the guard must not think Ctrl is held.
func TestSecureScreenResetsHeldKeys(t *testing.T) {
	for _, reset := range []bool{false, true} {
		r := newReplay()
		r.lock(false)
		r.g.OpenPrompt(r.now)
		r.press(VKLControl, VKLMenu, VKDelete) // the releases happen on Windows' own screen
		if reset {
			r.g.ResetHeld() // the app does this when Windows switches back to the desktop
		}
		r.tap('O', 'K')
		typed := r.g.prompt.Candidate() == "ok"
		if typed != reset {
			t.Errorf("reset %v: typed %q", reset, r.g.prompt.Candidate())
		}
	}
	// Opening the prompt with clicks also starts from nothing held.
	r := newReplay()
	r.lock(false)
	r.press(VKLMenu)
	r.clicks(5, 100*time.Millisecond)
	r.tap('O', 'K')
	if r.g.prompt.Candidate() != "ok" {
		t.Errorf("five clicks: typed %q", r.g.prompt.Candidate())
	}
}

func TestClicksStayBlockedAndOpenThePrompt(t *testing.T) {
	r := newReplay()
	r.lock(false)
	for _, kind := range []MouseKind{MouseMove, MouseWheel} {
		if r.mouse(kind, 0, false, false).Pass || r.mouse(kind, 0, true, false).Pass {
			t.Error("pointer moves and the wheel stay blocked, from the mouse or software")
		}
	}
	for _, b := range []uint32{VKRButton, VKMButton, VKXButton1, VKXButton2} {
		for range 5 {
			r.mouse(MouseDown, b, false, false)
			r.mouse(MouseUp, b, false, false)
		}
	}
	if r.prompts != 0 {
		t.Error("only left clicks count")
	}
	for range 5 {
		r.mouse(MouseDown, VKLButton, false, true)
		r.mouse(MouseUp, VKLButton, false, true)
	}
	if r.prompts != 0 {
		t.Error("clicks Windows makes from touches are not counted twice; the shield counts the taps")
	}
	r.now += 4 * time.Second
	for i := range 4 {
		r.now += 100 * time.Millisecond
		if e := r.g.Touch(r.now); e.ShowPrompt || !e.Counted || r.g.Clicks(r.now) != i+1 {
			t.Errorf("tap %d: %+v, %d counted", i+1, e, r.g.Clicks(r.now))
		}
	}
	if e := r.g.Touch(r.now + 100*time.Millisecond); !e.ShowPrompt || e.Counted {
		t.Error("five quick taps on the touch shield open the prompt")
	}
	r.key(VKEscape, false, false)
	r.key(VKEscape, true, false)
	r.now += 2 * time.Second
	r.mouse(MouseDown, VKLButton, true, false) // a remote-control click counts too
	r.mouse(MouseUp, VKLButton, true, false)
	r.clicks(4, 300*time.Millisecond)
	if r.prompts != 1 || len(r.leaked) != 0 || len(r.apps) != 0 {
		t.Errorf("five clicks from the mouse or remote control open the prompt and stay blocked: prompts %d leaked %v", r.prompts, r.leaked)
	}
	r.clicks(5, 100*time.Millisecond)
	if r.prompts != 1 {
		t.Error("clicks don't count while the prompt is open")
	}
	var unlocked Guard
	if !unlocked.Key(KeyEvent{VK: 'A'}, 0).Pass || !unlocked.Mouse(MouseEvent{Kind: MouseDown, Button: VKLButton}, 0).Pass ||
		unlocked.Touch(0) != (Effects{}) || unlocked.Clicks(0) != 0 {
		t.Error("nothing is blocked or counted while unlocked")
	}
}

// Seeded random sessions from the keyboard, the mouse and software, with locks
// and unlocks in between: no press ever leaks, and once every key and button
// is up, apps agree nothing is held.
func TestRandomInput(t *testing.T) {
	seed := uint64(0x1D6A_2E55)
	next := func(n int) int {
		seed = seed*6364136223846793005 + 1442695040888963407
		return int((seed >> 33) % uint64(n))
	}
	keys := []uint32{VKLControl, VKRControl, VKLMenu, VKRMenu, VKLShift, VKRShift, VKLWin, VKShift, VKControl, VKMenu,
		'A', 'L', 'Q', VKReturn, VKEscape, VKBack, VKTab, VKCapital, VKSpace, VKPacket}
	buttons := []uint32{VKLButton, VKRButton, VKMButton}
	r := newReplay()
	events, outOfSync := 0, 0
	for range 50_000 {
		injected := next(3) == 0
		switch n := next(20); {
		case n < 12:
			vk := keys[next(len(keys))]
			r.keyEvent(KeyEvent{VK: vk, Scan: 'k', Up: r.down[canonical(vk)], Injected: injected})
		case n < 15:
			b := buttons[next(len(buttons))]
			kind := MouseDown
			if r.down[b] {
				kind = MouseUp
			}
			r.mouse(kind, b, injected, next(10) == 0)
		case n < 17:
			r.mouse(MouseMove, 0, injected, false)
		case n == 17:
			if r.g.Locked() && r.g.Verifying() {
				r.g.Verified(r.g.Session(), next(2) == 0, r.now)
			}
		default:
			if next(8) == 0 {
				if r.g.Locked() {
					r.g.Unlock()
				} else {
					r.lock(next(2) == 0)
				}
			}
		}
		events++
		if len(r.down) == 0 && len(r.apps) != 0 {
			outOfSync++
		}
	}
	if outOfSync != 0 || len(r.leaked) != 0 {
		t.Fatalf("%d events: out of sync %d times, leaked %v", events, outOfSync, r.leaked)
	}
	t.Logf("%d random events: nothing leaked, apps never kept a key or button once all were up (%d prompts)", events, r.prompts)
}

// The prompt reads keys as the US English layout types them.
func TestTypedCharLayout(t *testing.T) {
	type press struct {
		vk          uint32
		shift, caps bool
	}
	typeAll := func(presses []press) string {
		var out []byte
		for _, p := range presses {
			if c := TypedChar(p.vk, p.shift, p.caps); c != 0 {
				out = append(out, c)
			}
		}
		return string(out)
	}
	var plain, shifted []press
	for vk := uint32(0x20); vk <= 0xE2; vk++ {
		plain = append(plain, press{vk, false, false})
		shifted = append(shifted, press{vk, true, false})
	}
	if got := typeAll(plain); got != " 0123456789abcdefghijklmnopqrstuvwxyz0123456789*+-./;=,-./`[\\]'\\" {
		t.Errorf("unshifted keys type %q", got)
	}
	if got := typeAll(shifted); got != " )!@#$%^&*(ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789*+-./:+<_>?~{|}\"|" {
		t.Errorf("shifted keys type %q", got)
	}
	if TypedChar('Q', false, true) != 'Q' || TypedChar('Q', true, true) != 'q' || TypedChar('1', false, true) != '1' {
		t.Error("Caps Lock changes letters only, and Shift turns it back for them")
	}
	for _, vk := range []uint32{VKBack, VKTab, VKReturn, VKEscape, VKLShift, VKF4, VKPacket, 0xFF} {
		if TypedChar(vk, false, false) != 0 {
			t.Errorf("%#x types nothing", vk)
		}
	}
	var g Guard
	g.Lock(nil, false, false)
	if e := g.Key(KeyEvent{VK: 0x1234}, 0); e.Pass {
		t.Error("an out-of-range key code stays blocked")
	}
}

// Over remote control the cursor doesn't move while locked, so each counted
// click is reported, and the notice shows how many of the five have landed.
func TestClickProgress(t *testing.T) {
	var g Guard
	g.Lock(nil, false, false)
	click := func(at time.Duration, b uint32, injected bool) Effects {
		g.Mouse(MouseEvent{Kind: MouseUp, Button: b, Injected: injected}, at)
		return g.Mouse(MouseEvent{Kind: MouseDown, Button: b, Injected: injected}, at)
	}
	ms := time.Millisecond
	for i, at := range []time.Duration{0, 400 * ms, 800 * ms, 1200 * ms} {
		e := click(at, VKLButton, true) // a click from remote control, blocked
		if e.Pass || !e.Counted || e.ShowPrompt || g.Clicks(at) != i+1 {
			t.Fatalf("click %d: %+v, %d of 5 shown", i+1, e, g.Clicks(at))
		}
	}
	if g.Clicks(3100*ms) != 3 || g.Clicks(4300*ms) != 0 {
		t.Error("clicks older than 3 seconds drop out of what the notice shows")
	}
	if e := click(1500*ms, VKRButton, false); e.Counted || g.Clicks(1500*ms) != 4 {
		t.Error("a right click isn't counted")
	}
	if e := click(1600*ms, VKLButton, false); !e.ShowPrompt || e.Counted || g.Clicks(1600*ms) != 0 {
		t.Errorf("the fifth click opens the prompt: %+v", e)
	}
	if e := click(1700*ms, VKLButton, false); e.Counted || g.Clicks(1700*ms) != 0 {
		t.Error("nothing is counted or shown while the prompt is open")
	}
	var auto Guard
	auto.Lock(nil, false, true)
	if e := auto.Mouse(MouseEvent{Kind: MouseDown, Button: VKLButton}, 0); e.Counted || auto.Clicks(0) != 0 {
		t.Error("with automation allowed, clicks aren't counted or shown")
	}
}
