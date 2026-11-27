package core

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// The recovery helper is a second copy of InputGuard.exe, started with
// --recovery before the hooks go in, as on the Mac. The app sends a heartbeat
// from its UI thread four times a second. If the heartbeats stop for 3
// seconds, the pipe breaks, or a trial reaches its 15 seconds, the helper ends
// the app, and Windows removes the app's hooks and windows with it. If the
// helper stops answering for 2 seconds, the app ends the lock itself.
const (
	HeartbeatTimeout = 3 * time.Second
	AckTimeout       = 2 * time.Second
	MaxTimedSession  = 300 * time.Second
	TrialLength      = 15 * time.Second
	startupTimeout   = 3 * time.Second // Windows can take a moment to start a process
	stopGrace        = 3 * time.Second
)

var ErrRecoveryNotStarted = errors.New("The independent recovery process did not start. Input blocking was not started.")

// Clock: Now includes sleep and ignores changes to the wall clock, for
// deadlines. Heartbeat leaves out sleep, so a sleeping PC is not taken for a
// frozen app.
type Clock interface {
	Now() time.Duration
	Heartbeat() time.Duration
}

// Parent is the app, as the helper sees it.
type Parent interface {
	Alive() bool
	Kill()
}

// RecoveryArg is the helper's one argument: "until-unlock", or a timed
// session's length in seconds.
func RecoveryArg(length time.Duration) string {
	if length <= 0 {
		return "until-unlock"
	}
	return strconv.FormatFloat(length.Seconds(), 'f', -1, 64)
}

// Supervisor is the app's end of the helper.
type Supervisor struct {
	cmd      *exec.Cmd
	clock    Clock
	deadline time.Duration
	lastAck  atomic.Int64
	failed   atomic.Bool
	exited   chan struct{}
	mu       sync.Mutex
	commands chan byte
	stopped  bool
}

// StartRecovery starts the helper and waits up to 3 seconds for it to say
// it is ready. length 0 means no deadline (a normal lock).
func StartRecovery(command func(arg string) *exec.Cmd, length time.Duration, clock Clock) (*Supervisor, error) {
	if length < 0 || length > MaxTimedSession || (length > 0 && length < time.Second) {
		return nil, errors.New("Invalid recovery session. Input blocking was not started.")
	}
	cmd := command(RecoveryArg(length))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, ErrRecoveryNotStarted
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, ErrRecoveryNotStarted
	}
	cmd.Stderr = nil
	s := &Supervisor{cmd: cmd, clock: clock, exited: make(chan struct{}), commands: make(chan byte, 64)}
	if length > 0 {
		s.deadline = clock.Now() + length
	}
	if err := cmd.Start(); err != nil {
		return nil, ErrRecoveryNotStarted
	}
	ready := make(chan bool, 1)
	go s.read(stdout, ready)
	go s.write(stdin)
	select {
	case ok := <-ready:
		if ok {
			s.lastAck.Store(int64(clock.Heartbeat()))
			return s, nil
		}
	case <-time.After(startupTimeout):
	}
	s.Stop()
	_ = cmd.Process.Kill()
	return nil, ErrRecoveryNotStarted
}

func (s *Supervisor) read(stdout io.Reader, ready chan<- bool) {
	buf := make([]byte, 256)
	first := true
	for {
		n, err := stdout.Read(buf)
		for _, b := range buf[:n] {
			switch {
			case first:
				first = false
				if b != 'R' {
					s.failed.Store(true)
				}
				ready <- b == 'R'
			case b == 'A':
				s.lastAck.Store(int64(s.clock.Heartbeat()))
			default:
				s.failed.Store(true)
			}
		}
		if err != nil {
			break
		}
	}
	s.failed.Store(true)
	if first {
		ready <- false
	}
	// Reading until the helper exits keeps the pipe open, so its last
	// answers can't fail and be taken for a broken app during a stop.
	_ = s.cmd.Wait()
	close(s.exited)
}

// write sends heartbeats, then the disarm D last, then closes the pipe.
func (s *Supervisor) write(stdin io.WriteCloser) {
	for b := range s.commands {
		if _, err := stdin.Write([]byte{b}); err != nil {
			s.failed.Store(true)
		}
	}
	_, _ = stdin.Write([]byte{'D'})
	_ = stdin.Close()
}

// Pulse checks the helper and sends one heartbeat. Call it only from the UI
// thread's timer: a heartbeat from anywhere else would hide a frozen UI.
func (s *Supervisor) Pulse() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.failed.Load() {
		return false
	}
	if s.deadline > 0 && s.clock.Now() >= s.deadline {
		return false
	}
	if s.clock.Heartbeat()-time.Duration(s.lastAck.Load()) >= AckTimeout {
		return false
	}
	select {
	case s.commands <- 'H':
		return true
	default:
		return false // the helper stopped reading
	}
}

// Stop disarms the helper. Call it only after the hooks are removed. A helper
// that doesn't exit within 3 seconds is ended; input is already free then.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.commands)
	go func() {
		select {
		case <-s.exited:
		case <-time.After(stopGrace):
			_ = s.cmd.Process.Kill()
		}
	}()
}

// Deadline is when a timed session ends on the app's clock, or 0.
func (s *Supervisor) Deadline() time.Duration { return s.deadline }

func (s *Supervisor) HelperPID() int { return s.cmd.Process.Pid }

// RunHelper is the helper's whole life. It reads H (heartbeat) and D (disarm)
// and answers R (ready) once, then A for each heartbeat. It ends the app when
// heartbeats stop for 3 seconds, the pipe breaks or carries anything else, or
// a timed session reaches its deadline. It never touches input, windows or
// files. It returns the exit code.
func RunHelper(arg string, in io.Reader, out io.Writer, parent Parent, clock Clock) int {
	var deadline time.Duration
	if arg != "until-unlock" {
		seconds, err := strconv.ParseFloat(arg, 64)
		if err != nil || math.IsNaN(seconds) || seconds < 1 || seconds > MaxTimedSession.Seconds() {
			return 64
		}
		deadline = clock.Now() + time.Duration(seconds*float64(time.Second))
	}
	if parent == nil || !parent.Alive() {
		return 65
	}
	chunks := make(chan []byte, 64)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				chunks <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				close(chunks)
				return
			}
		}
	}()
	if _, err := out.Write([]byte{'R'}); err != nil {
		return 66
	}
	lastBeat := clock.Heartbeat()
	rescue := func() int {
		if parent.Alive() {
			parent.Kill()
		}
		return 0
	}
	// handle returns done when the helper should exit now.
	handle := func(chunk []byte, open bool) (int, bool) {
		if !open {
			return rescue(), true // the pipe closed without a disarm
		}
		if bytes.IndexByte(chunk, 'D') >= 0 {
			return 0, true // sent only after the app removed its hooks
		}
		for _, b := range chunk {
			if b != 'H' {
				return rescue(), true
			}
		}
		lastBeat = clock.Heartbeat()
		if _, err := out.Write([]byte{'A'}); err != nil {
			return rescue(), true
		}
		return 0, false
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if !parent.Alive() {
			return 0
		}
		// Whatever already arrived comes first: a disarm wins over a deadline.
		for pending := true; pending; {
			select {
			case chunk, open := <-chunks:
				if code, done := handle(chunk, open); done {
					return code
				}
			default:
				pending = false
			}
		}
		if (deadline > 0 && clock.Now() >= deadline) || clock.Heartbeat()-lastBeat >= HeartbeatTimeout {
			return rescue()
		}
		select {
		case chunk, open := <-chunks:
			if code, done := handle(chunk, open); done {
				return code
			}
		case <-tick.C:
		}
	}
}
