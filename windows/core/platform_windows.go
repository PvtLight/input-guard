//go:build windows

package core

import (
	"errors"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	started = time.Now()
	// kernel32 is in every process already and always comes from System32.
	procQueryUnbiasedInterruptTime = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryUnbiasedInterruptTime")
)

type systemClock struct{}

var SystemClock Clock = systemClock{}

// Now is Go's monotonic clock, which on Windows counts sleep too.
func (systemClock) Now() time.Duration { return time.Since(started) }

// Heartbeat is QueryUnbiasedInterruptTime, which leaves out sleep.
func (systemClock) Heartbeat() time.Duration {
	var ticks uint64 // 100-nanosecond units
	if ok, _, _ := procQueryUnbiasedInterruptTime.Call(uintptr(unsafe.Pointer(&ticks))); ok == 0 {
		return time.Since(started)
	}
	return time.Duration(ticks) * 100
}

type windowsParent struct{ handle syscall.Handle }

// OpenParent opens the app's process while it waits for the helper to say it
// is ready. Holding the handle keeps Windows from reusing its PID, so the
// helper can never end some other process.
func OpenParent() (Parent, error) {
	pid := os.Getppid()
	if pid <= 0 {
		return nil, errors.New("no parent")
	}
	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE|syscall.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return nil, err
	}
	return windowsParent{handle}, nil
}

func (p windowsParent) Alive() bool {
	event, err := syscall.WaitForSingleObject(p.handle, 0)
	return err == nil && event == syscall.WAIT_TIMEOUT
}

func (p windowsParent) Kill() { _ = syscall.TerminateProcess(p.handle, 1) }
