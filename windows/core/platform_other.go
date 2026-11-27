//go:build !windows

package core

import (
	"os"
	"syscall"
	"time"
)

var started = time.Now()

type systemClock struct{}

// SystemClock on other systems is only used by the tests.
var SystemClock Clock = systemClock{}

func (systemClock) Now() time.Duration       { return time.Since(started) }
func (systemClock) Heartbeat() time.Duration { return time.Since(started) }

type unixParent struct{ pid int }

// OpenParent remembers the process that started the helper. When that process
// ends, the helper is given a new parent, so a reused PID is never ended.
func OpenParent() (Parent, error) { return unixParent{os.Getppid()}, nil }

func (p unixParent) Alive() bool { return p.pid > 1 && os.Getppid() == p.pid }
func (p unixParent) Kill()       { _ = syscall.Kill(p.pid, syscall.SIGKILL) }
