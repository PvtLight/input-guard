package core

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

const s = time.Second

func TestPassphraseRecord(t *testing.T) {
	vector, err := Derive("password", []byte("salt"), 1)
	if err != nil || hex.EncodeToString(vector) != "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b" {
		t.Fatalf("PBKDF2-SHA256 known answer: got %x, %v", vector, err)
	}
	record, err := NewRecord("a long test passphrase!")
	if err != nil {
		t.Fatal(err)
	}
	if record.Rounds != 600_000 || len(record.Salt) != 32 || len(record.Digest) != 32 {
		t.Fatalf("record shape: %d rounds, %d-byte salt, %d-byte digest", record.Rounds, len(record.Salt), len(record.Digest))
	}
	if !record.Matches("a long test passphrase!") {
		t.Error("the right passphrase must match")
	}
	if record.Matches("a long wrong passphrase!") || record.Matches("") {
		t.Error("a wrong or empty passphrase must not match")
	}
	other, _ := NewRecord("a long test passphrase!")
	if string(other.Salt) == string(record.Salt) {
		t.Error("every record gets its own random salt")
	}
	short, err := NewRecord("2468")
	if err != nil || !short.Matches("2468") || short.Matches("2469") {
		t.Errorf("four characters can be saved and checked: %v", err)
	}
	for _, bad := range []string{"123", "漢字 passphrase long", "tab\there", strings.Repeat("x", 129)} {
		if ValidPassphrase(bad) {
			t.Errorf("%q must be refused", bad)
		}
		if _, err := NewRecord(bad); err != ErrPassphraseRules {
			t.Errorf("%q: want the rules message, got %v", bad, err)
		}
	}
	if !ValidPassphrase(strings.Repeat("x", 128)) || !ValidPassphrase("Open sesame 42!~") {
		t.Error("128 printable ASCII characters are allowed")
	}
	corrupt := Record{Version: 1, Rounds: 1}
	if corrupt.Matches("anything") || corrupt.Validate() == nil {
		t.Error("a damaged record fails closed")
	}
	weak := record
	weak.Rounds = 1
	if weak.Validate() == nil || weak.Matches("a long test passphrase!") {
		t.Error("a record with fewer rounds is refused")
	}
}

func TestAttemptLimiter(t *testing.T) {
	var limiter AttemptLimiter
	limiter.Failed(10 * s)
	if limiter.CanTry(11*s) || !limiter.CanTry(12*s) {
		t.Error("the first wrong passphrase waits 2 seconds")
	}
	var waits []time.Duration
	limiter = AttemptLimiter{}
	for range 7 {
		limiter.Failed(100 * s)
		waits = append(waits, limiter.NextAttempt-100*s)
	}
	want := []time.Duration{2 * s, 4 * s, 8 * s, 16 * s, 30 * s, 30 * s, 30 * s}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits %v, want %v", waits, want)
		}
	}
}

// Replays the unlock prompt with a real record and limiter, the way the app
// answers each Enter.
func TestUnlockPrompt(t *testing.T) {
	record, err := NewRecord("Open sesame 42")
	if err != nil {
		t.Fatal(err)
	}
	enter := func(p *UnlockPrompt, l *AttemptLimiter, now time.Duration) bool {
		submitted, ok := p.Submit(*l, now)
		if !ok {
			return false
		}
		if record.Matches(submitted) {
			return true
		}
		p.Rejected(l, now)
		return false
	}

	var limiter AttemptLimiter
	prompt := NewUnlockPrompt(false)
	if enter(&prompt, &limiter, 10*s) || limiter.Failures != 0 || !limiter.CanTry(10*s) || prompt.Message != PromptInstructions {
		t.Error("Enter with nothing typed is not checked, counted or made to wait")
	}

	limiter, prompt = AttemptLimiter{}, NewUnlockPrompt(false)
	prompt.Type("wrong guess")
	enter(&prompt, &limiter, 10*s) // must wait until 12
	prompt.Type("Open sesame 42")
	early := enter(&prompt, &limiter, 11*s)
	kept := prompt.Candidate() == "Open sesame 42" && strings.HasSuffix(prompt.Message, "Press Enter again in 1 second.")
	later := enter(&prompt, &limiter, 12*s)
	if early || !kept || !later || limiter.Failures != 1 {
		t.Errorf("Enter during a wait keeps what was typed, then unlocks after it (early %v kept %v later %v)", early, kept, later)
	}

	prompt = NewUnlockPrompt(true)
	shown := prompt.Note() == PromptInstructions+"\nCaps Lock is on."
	prompt.Type("OPEN")
	enter(&prompt, &limiter, 20*s)
	afterMismatch := strings.HasPrefix(prompt.Note(), "That passphrase did not match.") && strings.HasSuffix(prompt.Note(), "\nCaps Lock is on.")
	prompt.CapsLock = false
	if !shown || !afterMismatch || strings.Contains(prompt.Note(), "Caps Lock") {
		t.Error("Caps Lock is pointed out while on, also after a mismatch, and cleared when off")
	}

	limiter, prompt = AttemptLimiter{}, NewUnlockPrompt(false)
	prompt.Type("not it")
	if enter(&prompt, &limiter, 50*s) || limiter.Failures != 1 || limiter.CanTry(51*s) || prompt.Length() != 0 ||
		prompt.Message != "That passphrase did not match. Wait 2 seconds, then try again." {
		t.Errorf("a wrong passphrase counts, clears and starts a 2-second wait: %q", prompt.Message)
	}
	limiter, prompt = AttemptLimiter{}, NewUnlockPrompt(false)
	prompt.Type("Open sesame 42")
	if !enter(&prompt, &limiter, 0) || limiter.Failures != 0 || prompt.Length() != 0 {
		t.Error("the right passphrase unlocks on the first try")
	}

	typing := NewUnlockPrompt(false)
	for _, text := range []string{"ab", "é", "\x7f", "", "ø", " C~"} {
		typing.Type(text)
	}
	typing.DeleteLast()
	long := NewUnlockPrompt(false)
	for range 130 {
		long.Type("x")
	}
	if typing.Candidate() != "ab C" || long.Length() != 128 {
		t.Errorf("typing takes printable ASCII only, up to 128, and Backspace removes one: %q %d", typing.Candidate(), long.Length())
	}
	if Seconds(1500*time.Millisecond) != "2 seconds" || Seconds(0) != "1 second" {
		t.Error("waits round up to whole seconds")
	}
}

func TestFiveClickTiming(t *testing.T) {
	// Which clicks, counting from 1, open the prompt. Times in milliseconds.
	opens := func(times ...int) []int {
		var counter ClickUnlock
		var opened []int
		for i, ms := range times {
			if counter.Click(time.Duration(ms) * time.Millisecond) {
				opened = append(opened, i+1)
			}
		}
		return opened
	}
	steady := []int{}
	for ms := 0; ms < 60_000; ms += 800 {
		steady = append(steady, ms)
	}
	quick := []int{}
	for ms := 0; ms < 2000; ms += 200 {
		quick = append(quick, ms)
	}
	cases := []struct {
		name  string
		times []int
		want  []int
	}{
		{"five quick clicks open the prompt on the fifth", []int{0, 200, 400, 600, 800}, []int{5}},
		{"four clicks do nothing", []int{0, 200, 400, 600}, nil},
		{"steady clicking, one every 0.8 seconds for a minute, never opens it", steady, nil},
		{"clicks bunched up by remote-control lag still count", []int{0, 1900, 1950, 2000, 2050}, []int{5}},
		{"the count starts over: ten quick clicks open it twice", quick, []int{5, 10}},
		{"clicks older than 3 seconds drop out", []int{0, 100, 200, 300, 3400, 3500, 3600, 3700, 3800}, []int{9}},
		{"exactly 3 seconds apart is too slow", []int{0, 750, 1500, 2250, 3000}, nil},
	}
	for _, c := range cases {
		got := opens(c.times...)
		if len(got) != len(c.want) || (len(got) > 0 && got[0] != c.want[0]) || (len(got) > 1 && got[1] != c.want[1]) {
			t.Errorf("%s: opened on %v, want %v", c.name, got, c.want)
		}
	}
	if !ClicksCount(false, false) || ClicksCount(true, false) || ClicksCount(false, true) {
		t.Error("clicks count only while the prompt is closed and automation is not allowed")
	}
}
