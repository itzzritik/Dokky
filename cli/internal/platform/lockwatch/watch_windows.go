package lockwatch

import (
	"context"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmClose            = 0x0010
	wmPowerBroadcast   = 0x0218
	wmWTSSessionChange = 0x02B1

	pbtAPMSuspend = 0x0004

	wtsConsoleDisconnect = 0x2
	wtsRemoteDisconnect  = 0x4
	wtsSessionLock       = 0x7

	hwndMessage = ^uintptr(2) // HWND_MESSAGE, -3
)

var (
	user32                        = windows.NewLazySystemDLL("user32.dll")
	wtsapi32                      = windows.NewLazySystemDLL("wtsapi32.dll")
	procRegisterClassExW          = user32.NewProc("RegisterClassExW")
	procCreateWindowExW           = user32.NewProc("CreateWindowExW")
	procDestroyWindow             = user32.NewProc("DestroyWindow")
	procDefWindowProcW            = user32.NewProc("DefWindowProcW")
	procGetMessageW               = user32.NewProc("GetMessageW")
	procDispatchMessageW          = user32.NewProc("DispatchMessageW")
	procPostMessageW              = user32.NewProc("PostMessageW")
	procPostQuitMessage           = user32.NewProc("PostQuitMessage")
	procRegisterSuspendResume     = user32.NewProc("RegisterSuspendResumeNotification")
	procUnregisterSuspendResume   = user32.NewProc("UnregisterSuspendResumeNotification")
	procWTSRegisterSessionNotif   = wtsapi32.NewProc("WTSRegisterSessionNotification")
	procWTSUnRegisterSessionNotif = wtsapi32.NewProc("WTSUnRegisterSessionNotification")
)

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// only the one watchOS goroutine touches these; Watch runs once for the life of the daemon
var (
	onLockEvent   func()
	windowProc    = windows.NewCallback(handleMessage)
	className, _  = windows.UTF16PtrFromString("DokkyLockWatch")
	classDeclared bool
)

func watchOS(ctx context.Context, onLock func()) {
	onLockEvent = onLock
	// WTS registration fails until Remote Desktop Services has started, so a failure tries again later
	keep(ctx, runWindow)
}

func handleMessage(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case wmWTSSessionChange:
		switch wParam {
		case wtsSessionLock, wtsConsoleDisconnect, wtsRemoteDisconnect:
			onLockEvent()
		}
		return 0
	case wmPowerBroadcast:
		if wParam == pbtAPMSuspend {
			onLockEvent()
		}
		return 1
	case wmClose:
		_, _, _ = procDestroyWindow.Call(hwnd)
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

// session changes reach the registered window directly; a power broadcast never reaches a message-only window,
// hence the suspend registration
func runWindow(ctx context.Context) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !classDeclared {
		class := wndClassEx{wndProc: windowProc, className: className}
		class.size = uint32(unsafe.Sizeof(class))
		atom, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class)))
		if classDeclared = atom != 0; !classDeclared {
			return
		}
	}
	hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, hwndMessage, 0, 0, 0)
	if hwnd == 0 {
		return
	}
	if ok, _, _ := procWTSRegisterSessionNotif.Call(hwnd, 0); ok == 0 {
		_, _, _ = procDestroyWindow.Call(hwnd)
		return
	}
	defer procWTSUnRegisterSessionNotif.Call(hwnd)
	if handle, _, _ := procRegisterSuspendResume.Call(hwnd, 0); handle != 0 {
		defer procUnregisterSuspendResume.Call(handle)
	}
	stop := context.AfterFunc(ctx, func() { _, _, _ = procPostMessageW.Call(hwnd, wmClose, 0, 0) })
	defer stop()
	var msg winMsg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			return
		}
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
