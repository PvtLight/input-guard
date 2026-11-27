package core

import (
	"fmt"
	"math"
	"time"
)

// AttemptLimiter makes each wrong passphrase wait longer before the next try:
// 2, 4, 8 and 16 seconds, then 30 seconds from the fifth on, as on the Mac.
type AttemptLimiter struct {
	Failures    int
	NextAttempt time.Duration
}

func (l *AttemptLimiter) Failed(now time.Duration) {
	l.Failures++
	wait := time.Duration(1<<min(l.Failures, 5)) * time.Second
	l.NextAttempt = now + min(wait, 30*time.Second)
}

func (l AttemptLimiter) CanTry(now time.Duration) bool { return now >= l.NextAttempt }

const PromptInstructions = "Type your passphrase, then press Enter.\nEsc returns to Agent working."

// UnlockPrompt is the typed passphrase and the message under it. Enter with
// nothing typed is not a guess, Enter during a wait keeps what was typed, and
// the prompt says when Caps Lock is on.
type UnlockPrompt struct {
	candidate []byte
	Message   string
	CapsLock  bool
}

func NewUnlockPrompt(capsLock bool) UnlockPrompt {
	return UnlockPrompt{Message: PromptInstructions, CapsLock: capsLock}
}

func (p *UnlockPrompt) Note() string {
	if p.CapsLock {
		return p.Message + "\nCaps Lock is on."
	}
	return p.Message
}

// Length is how many characters are typed, for the dots.
func (p *UnlockPrompt) Length() int { return len(p.candidate) }

func (p *UnlockPrompt) Candidate() string { return string(p.candidate) }

// Type takes printable ASCII only, up to 128 characters, as saved passphrases are.
func (p *UnlockPrompt) Type(text string) {
	if text == "" || len(p.candidate)+len(text) > MaxPassphrase {
		return
	}
	for i := 0; i < len(text); i++ {
		if text[i] < 32 || text[i] > 126 {
			return
		}
	}
	p.candidate = append(p.candidate, text...)
}

func (p *UnlockPrompt) DeleteLast() {
	if n := len(p.candidate); n > 0 {
		p.candidate[n-1] = 0
		p.candidate = p.candidate[:n-1]
	}
}

func (p *UnlockPrompt) Show(text string) { p.Message = text }

// Clear overwrites what was typed. Go does not promise to wipe every copy.
func (p *UnlockPrompt) Clear() {
	for i := range p.candidate {
		p.candidate[i] = 0
	}
	p.candidate = p.candidate[:0]
}

// Submit returns the passphrase to check now, or false when there is nothing
// to check.
func (p *UnlockPrompt) Submit(limiter AttemptLimiter, now time.Duration) (string, bool) {
	if len(p.candidate) == 0 {
		return "", false
	}
	if !limiter.CanTry(now) {
		p.Message = "Too soon. What you typed is kept. Press Enter again in " + Seconds(limiter.NextAttempt-now) + "."
		return "", false
	}
	submitted := string(p.candidate)
	p.Clear()
	p.Message = "Checking passphrase…"
	return submitted, true
}

// Rejected counts a passphrase that did not match as a failed attempt.
func (p *UnlockPrompt) Rejected(limiter *AttemptLimiter, now time.Duration) {
	limiter.Failed(now)
	p.Message = "That passphrase did not match. Wait " + Seconds(limiter.NextAttempt-now) + ", then try again."
}

func Seconds(d time.Duration) string {
	count := max(1, int(math.Ceil(d.Seconds())))
	if count == 1 {
		return "1 second"
	}
	return fmt.Sprintf("%d seconds", count)
}

const (
	UnlockClicks      = 5
	UnlockClickWindow = 3 * time.Second
)

// ClickUnlock opens the unlock prompt on five quick clicks within three
// seconds, for remote control that can't send the shortcut. The clicks never
// unlock and stay blocked from apps. Only clicks the guard blocks are counted;
// nothing is watched while unlocked.
type ClickUnlock struct {
	recent []time.Duration
}

// ClicksCount: clicks count while the prompt is closed, and not with
// automation allowed: injected clicks pass then, and a prompt opened by stray
// clicks at the PC would pause that automation until someone answered it.
func ClicksCount(allowAutomation, enteringPassword bool) bool {
	return !allowAutomation && !enteringPassword
}

// Pending is how many clicks count right now, out of five.
func (c *ClickUnlock) Pending(now time.Duration) int {
	n := 0
	for _, t := range c.recent {
		if now-t < UnlockClickWindow {
			n++
		}
	}
	return n
}

// Click is true on the click that makes five within three seconds. The count
// then starts over, so opening the prompt again takes five more.
func (c *ClickUnlock) Click(now time.Duration) bool {
	kept := c.recent[:0]
	for _, t := range c.recent {
		if now-t < UnlockClickWindow {
			kept = append(kept, t)
		}
	}
	c.recent = append(kept, now)
	if len(c.recent) < UnlockClicks {
		return false
	}
	c.recent = c.recent[:0]
	return true
}
