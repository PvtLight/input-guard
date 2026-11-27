//go:build windows

// Input Guard for Windows: a notification-area app that blocks this PC's own
// keyboard, mouse, touchpad and touch screen while agents keep working, until
// the passphrase is typed. See ../README.md.
package main

import (
	"os"
	"runtime"
	"syscall"

	"inputguard/core"
)

const version = "0.1.0"

// The UI and its windows live on the main thread.
func init() { runtime.LockOSThread() }

// instanceMutex stays open for the life of the app.
var instanceMutex uintptr

func main() {
	// Load system DLLs only from System32, never from the folder the .exe is in.
	call(pSetDefaultDllDirectories, loadLibrarySearchSystem32)
	if len(os.Args) == 3 && os.Args[1] == "--recovery" {
		// The recovery helper: no windows, no hooks, no files.
		parent, err := core.OpenParent()
		if err != nil {
			os.Exit(65)
		}
		os.Exit(core.RunHelper(os.Args[2], os.Stdin, os.Stdout, parent, core.SystemClock))
	}
	if !claimInstance() {
		return
	}
	runApp()
}

// claimInstance lets one copy run. A second launch brings up the first one's
// settings window instead.
func claimInstance() bool {
	handle, _, err := pCreateMutexW.Call(0, 0, uintptr(wsz(`Local\InputGuard.Windows.SingleInstance`)))
	if handle != 0 && err != syscall.Errno(errorAlreadyExists) {
		instanceMutex = handle
		return true
	}
	if other := call(pFindWindowW, uintptr(wsz("InputGuardMain")), 0); other != 0 {
		postMessage(other, wmShowSettings, 0, 0)
	} else {
		messageBox("Input Guard is already running. Look for its icon in the notification area by the clock.", "Input Guard", mbIconInfo)
	}
	return false
}
