//go:build !windows

package core

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// The test binary also plays the app ("fixture") and the helper, as the Mac
// app's recovery tests do. The helper may end only the disposable fixture
// that started it. Nothing here blocks input.
func TestMain(m *testing.M) {
	switch os.Getenv("INPUTGUARD_TEST_ROLE") {
	case "helper":
		parent, err := OpenParent()
		if err != nil {
			os.Exit(65)
		}
		os.Exit(RunHelper(os.Getenv("INPUTGUARD_TEST_ARG"), os.Stdin, os.Stdout, parent, SystemClock))
	case "fixture":
		os.Exit(runFixture(os.Getenv("INPUTGUARD_TEST_MODE")))
	}
	os.Exit(m.Run())
}

func helperCommand(arg string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "INPUTGUARD_TEST_ROLE=helper", "INPUTGUARD_TEST_ARG="+arg)
	return cmd
}

func runFixture(mode string) int {
	if mode == "missing-helper" {
		missing := func(arg string) *exec.Cmd { return exec.Command("/no-such-input-guard-helper", arg) }
		if _, err := StartRecovery(missing, 3*time.Second, SystemClock); err == nil {
			return 10
		}
		return 0
	}
	length := 15 * time.Second
	switch mode {
	case "freeze", "indefinite-healthy":
		length = 0
	case "expiry":
		length = 2 * time.Second
	}
	supervisor, err := StartRecovery(helperCommand, length, SystemClock)
	if err != nil {
		return 15
	}
	if mode == "indefinite-healthy" && supervisor.Deadline() != 0 {
		return 16
	}
	switch mode {
	case "freeze":
		_ = syscall.Kill(os.Getpid(), syscall.SIGSTOP)
		return 11
	case "helper-exit":
		_ = syscall.Kill(supervisor.HelperPID(), syscall.SIGKILL)
	case "helper-freeze":
		_ = syscall.Kill(supervisor.HelperPID(), syscall.SIGSTOP)
	}
	start := time.Now()
	for time.Since(start) < 6*time.Second {
		if !supervisor.Pulse() {
			// The real app removes its hooks before stopping the helper here.
			supervisor.Stop()
			if mode == "helper-freeze" {
				_ = syscall.Kill(supervisor.HelperPID(), syscall.SIGCONT)
				return 0
			}
			if mode == "helper-exit" {
				return 0
			}
			if mode != "expiry" {
				return 12
			}
		}
		if mode == "clean-stop" && time.Since(start) > 500*time.Millisecond {
			supervisor.Stop()
			time.Sleep(3500 * time.Millisecond)
			return 0
		}
		if mode == "indefinite-healthy" && time.Since(start) > 4*time.Second {
			supervisor.Stop()
			return 0
		}
		// For expiry, stop heartbeats and ignore the app's own deadline, to
		// test the helper's hard stop while the app is otherwise fine.
		if mode == "expiry" && time.Since(start) > 1500*time.Millisecond {
			time.Sleep(5 * time.Second)
			return 13
		}
		time.Sleep(100 * time.Millisecond)
	}
	supervisor.Stop()
	return 14
}

func TestRecoveryHelper(t *testing.T) {
	cases := []struct {
		mode   string
		killed bool
		about  string
	}{
		{"freeze", true, "a frozen app in a normal lock is ended within about 3 seconds"},
		{"expiry", true, "the trial's hard deadline ends the app even while it runs"},
		{"helper-exit", false, "a killed helper makes the app end the lock itself"},
		{"helper-freeze", false, "a frozen helper makes the app end the lock itself"},
		{"clean-stop", false, "a clean stop disarms the helper"},
		{"missing-helper", false, "no helper means no lock"},
		{"indefinite-healthy", false, "a healthy normal lock has no deadline"},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			t.Parallel()
			fixture := exec.Command(os.Args[0], "-test.run=^$")
			fixture.Env = append(os.Environ(), "INPUTGUARD_TEST_ROLE=fixture", "INPUTGUARD_TEST_MODE="+c.mode)
			if err := fixture.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- fixture.Wait() }()
			var err error
			select {
			case err = <-done:
			case <-time.After(9 * time.Second):
				_ = fixture.Process.Kill()
				<-done
				t.Fatalf("%s: timed out", c.about)
			}
			var exit *exec.ExitError
			killed := errors.As(err, &exit) && exit.Sys().(syscall.WaitStatus).Signal() == syscall.SIGKILL
			if c.killed != killed || (!c.killed && err != nil) {
				t.Fatalf("%s: got %v", c.about, err)
			}
			t.Log(c.about)
		})
	}
}

func TestRecoveryArguments(t *testing.T) {
	alive := alwaysAlive{}
	for _, arg := range []string{"", "0", "0.5", "301", "NaN", "until-later", "-3"} {
		if code := RunHelper(arg, nil, nil, alive, SystemClock); code != 64 {
			t.Errorf("%q: exit %d, want 64", arg, code)
		}
	}
	if RecoveryArg(0) != "until-unlock" || RecoveryArg(TrialLength) != "15" {
		t.Error("helper arguments")
	}
	if _, err := StartRecovery(helperCommand, 400*time.Millisecond, SystemClock); err == nil {
		t.Error("a timed session shorter than a second is refused")
	}
}

type alwaysAlive struct{}

func (alwaysAlive) Alive() bool { return true }
func (alwaysAlive) Kill()       {}
