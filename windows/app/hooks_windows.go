//go:build windows

package main

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"inputguard/core"
)

// shared is the lock state the hooks and the UI thread both use. The hooks
// hold the mutex for microseconds; nothing slow ever runs under it.
var shared struct {
	sync.Mutex
	guard core.Guard
}

// Notice flags the hooks post to the main window with wmGuard.
const (
	noticeShow = 1 << iota
	noticeHide
	noticeVerify
	noticeRedraw
	noticeCounted
)

func effectsFlags(e core.Effects) uintptr {
	var f uintptr
	if e.ShowPrompt {
		f |= noticeShow
	}
	if e.HidePrompt {
		f |= noticeHide
	}
	if e.Verify {
		f |= noticeVerify
	}
	if e.Redraw {
		f |= noticeRedraw
	}
	if e.Counted {
		f |= noticeCounted
	}
	return f
}

var (
	keyboardCallback = syscall.NewCallback(keyboardProc)
	mouseCallback    = syscall.NewCallback(mouseProc)
)

// keyboardProc is the low-level keyboard hook. It decides and returns at
// once: anything slower risks Windows dropping the hook.
func keyboardProc(code, wParam, lParam uintptr) uintptr {
	if int32(code) == 0 { // HC_ACTION
		k := (*kbdllHookStruct)(unsafe.Pointer(lParam))
		ev := core.KeyEvent{VK: k.VkCode, Scan: k.ScanCode, Up: k.Flags&llkhfUp != 0, Injected: k.Flags&llkhfInjectedAny != 0}
		if inputCheck.active.Load() && !ev.Up {
			inputCheck.key(ev, k.ExtraInfo)
		}
		shared.Lock()
		e := shared.guard.Key(ev, core.SystemClock.Now())
		shared.Unlock()
		if f := effectsFlags(e); f != 0 {
			postMessage(mainWindow, wmGuard, f, 0)
		}
		if !e.Pass {
			return 1
		}
	}
	return call(pCallNextHookEx, 0, code, wParam, lParam)
}

// mouseProc is the low-level mouse hook, for the mouse, the touchpad and
// clicks from remote control.
func mouseProc(code, wParam, lParam uintptr) uintptr {
	if int32(code) == 0 {
		m := (*msllHookStruct)(unsafe.Pointer(lParam))
		ev := core.MouseEvent{
			Injected: m.Flags&llmhfInjectedAny != 0,
			Touch:    uint32(m.ExtraInfo)&touchSignatureMask == touchSignature,
		}
		switch wParam {
		case wmLButtonDown, wmLButtonUp:
			ev.Button = core.VKLButton
		case wmRButtonDown, wmRButtonUp:
			ev.Button = core.VKRButton
		case wmMButtonDown, wmMButtonUp:
			ev.Button = core.VKMButton
		case wmXButtonDown, wmXButtonUp:
			ev.Button = core.VKXButton1
			if hiword(uintptr(m.MouseData)) == 2 {
				ev.Button = core.VKXButton2
			}
		}
		switch wParam {
		case wmLButtonDown, wmRButtonDown, wmMButtonDown, wmXButtonDown:
			ev.Kind = core.MouseDown
		case wmLButtonUp, wmRButtonUp, wmMButtonUp, wmXButtonUp:
			ev.Kind = core.MouseUp
		case wmMouseWheel, wmMouseHWheel:
			ev.Kind = core.MouseWheel
		default:
			ev.Kind = core.MouseMove
		}
		if inputCheck.active.Load() {
			inputCheck.mouse(ev, m.ExtraInfo)
		}
		shared.Lock()
		e := shared.guard.Mouse(ev, core.SystemClock.Now())
		shared.Unlock()
		if f := effectsFlags(e); f != 0 {
			postMessage(mainWindow, wmGuard, f, 0)
		}
		if !e.Pass {
			return 1
		}
	}
	return call(pCallNextHookEx, 0, code, wParam, lParam)
}

// hookThread owns the two hooks. Windows calls a low-level hook on the
// thread that installed it, while that thread waits for messages, so this
// thread does nothing else and stays responsive even when the UI is busy.
type hookThread struct {
	tid      uint32
	keyboard atomic.Uintptr
	mouse    atomic.Uintptr
	beat     atomic.Int64
	done     chan struct{}
}

func startHooks() (*hookThread, error) {
	h := &hookThread{done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		// Never unlocked: when this goroutine ends, its thread ends with it.
		runtime.LockOSThread()
		defer close(h.done)
		var m msg
		call(pPeekMessageW, uintptr(unsafe.Pointer(&m)), 0, wmUser, wmUser, 0) // creates the message queue
		h.tid = uint32(call(pGetCurrentThreadId))
		// Windows waits for this thread on every key and mouse event, so a busy
		// PC must not keep it waiting. It sleeps the rest of the time.
		call(pSetThreadPriority, call(pGetCurrentThread), 15) // THREAD_PRIORITY_TIME_CRITICAL
		h.keyboard.Store(call(pSetWindowsHookExW, whKeyboardLL, keyboardCallback, instance, 0))
		h.mouse.Store(call(pSetWindowsHookExW, whMouseLL, mouseCallback, instance, 0))
		if h.keyboard.Load() == 0 || h.mouse.Load() == 0 {
			h.unhook()
			ready <- errors.New("Windows refused the input blocker. Input blocking was not started.")
			return
		}
		timer := call(pSetTimer, 0, 0, 250, 0)
		h.beat.Store(int64(core.SystemClock.Heartbeat()))
		ready <- nil
		for {
			r := int32(call(pGetMessageW, uintptr(unsafe.Pointer(&m)), 0, 0, 0))
			if r == 0 || r == -1 {
				break
			}
			if m.Message == wmTimer {
				h.beat.Store(int64(core.SystemClock.Heartbeat()))
			}
		}
		call(pKillTimer, 0, timer)
		h.unhook()
	}()
	if err := <-ready; err != nil {
		<-h.done
		return nil, err
	}
	return h, nil
}

func (h *hookThread) unhook() {
	if hook := h.keyboard.Swap(0); hook != 0 {
		call(pUnhookWindowsHookEx, hook)
	}
	if hook := h.mouse.Swap(0); hook != 0 {
		call(pUnhookWindowsHookEx, hook)
	}
}

// healthy is false when the hook thread stopped or its message loop stalled.
func (h *hookThread) healthy() bool {
	select {
	case <-h.done:
		return false
	default:
	}
	return core.SystemClock.Heartbeat()-time.Duration(h.beat.Load()) < 1500*time.Millisecond
}

// stop removes the hooks. The guard is already unlocked by then, so the hooks
// pass everything even before they are gone.
func (h *hookThread) stop() {
	call(pPostThreadMessageW, uintptr(h.tid), wmQuit, 0, 0)
	select {
	case <-h.done:
	case <-time.After(time.Second):
		// The thread is stuck: remove the hooks from here. Windows also drops
		// a low-level hook that stops answering.
		h.unhook()
	}
}
