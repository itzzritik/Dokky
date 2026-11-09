package sysauth

import (
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const helloPromptClass = "Credential Dialog Xaml Host"

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procFindWindowExW       = user32.NewProc("FindWindowExW")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procAttachThreadInput   = user32.NewProc("AttachThreadInput")
	procPeekMessageW        = user32.NewProc("PeekMessageW")
)

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// waits for the prompt window and raises it until it is in front, then stops so the user can still switch away
func raiseWhenShown(stop <-chan struct{}, find func() uintptr) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var hwnd uintptr
	for attempts := 0; attempts < 10; {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		if hwnd == 0 {
			if hwnd = find(); hwnd == 0 {
				continue
			}
		}
		if foreground, _, _ := procGetForegroundWindow.Call(); foreground == hwnd {
			return
		}
		bringToFront(hwnd)
		attempts++
	}
}

// sharing the foreground thread's input state for the call lets SetForegroundWindow past the foreground lock.
// injecting an ALT key instead is unsafe: once the elevated Hello dialog is in front, UIPI drops the key-up
// and the key stays held system wide
func bringToFront(hwnd uintptr) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	foreground, _, _ := procGetForegroundWindow.Call()
	if foreground == hwnd {
		return
	}
	// AttachThreadInput needs this thread to have a message queue
	var msg winMsg
	_, _, _ = procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 0)
	current := windows.GetCurrentThreadId()
	target, _ := windows.GetWindowThreadProcessId(windows.HWND(foreground), nil)
	if target != 0 && target != current {
		if attached, _, _ := procAttachThreadInput.Call(uintptr(current), uintptr(target), 1); attached != 0 {
			defer func() { _, _, _ = procAttachThreadInput.Call(uintptr(current), uintptr(target), 0) }()
		}
	}
	_, _, _ = procSetForegroundWindow.Call(hwnd)
}

// matches a Hello dialog that was not already open, so another app's prompt is never raised
func helloPromptFinder() func() uintptr {
	existing := findWindow(helloPromptClass, 0)
	return func() uintptr { return findWindow(helloPromptClass, existing) }
}

// a visible top-level window of class, other than exclude
func findWindow(class string, exclude uintptr) uintptr {
	name, err := windows.UTF16PtrFromString(class)
	if err != nil {
		return 0
	}
	var after uintptr
	for {
		hwnd, _, _ := procFindWindowExW.Call(0, after, uintptr(unsafe.Pointer(name)), 0)
		if hwnd == 0 {
			return 0
		}
		after = hwnd
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible != 0 && hwnd != exclude {
			return hwnd
		}
	}
}
