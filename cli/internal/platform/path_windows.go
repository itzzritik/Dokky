//go:build windows

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func EnsurePath(home, stateDir string) (bool, error) {
	bin := filepath.Join(home, ".local", "bin")
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if strings.EqualFold(p, bin) {
			return false, nil
		}
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()

	cur, typ, err := k.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, err
	}
	if hasEntry(cur, bin) {
		return false, nil
	}
	next := bin
	if cur != "" {
		next = strings.TrimRight(cur, ";") + ";" + bin
	}
	if typ == registry.SZ {
		err = k.SetStringValue("Path", next)
	} else {
		err = k.SetExpandStringValue("Path", next)
	}
	if err != nil {
		return false, err
	}
	broadcastEnvChange()
	return true, recordPathAdded(stateDir)
}

func RemovePath(home, stateDir string) error {
	bin := filepath.Join(home, ".local", "bin")
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	cur, typ, err := k.GetStringValue("Path")
	if err == nil {
		var keep []string
		for _, p := range strings.Split(cur, ";") {
			if p != "" && !strings.EqualFold(p, bin) {
				keep = append(keep, p)
			}
		}
		next := strings.Join(keep, ";")
		if next != cur {
			if typ == registry.SZ {
				err = k.SetStringValue("Path", next)
			} else {
				err = k.SetExpandStringValue("Path", next)
			}
			if err != nil {
				return err
			}
			broadcastEnvChange()
		}
	} else if !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return clearPathRecord(stateDir)
}

func hasEntry(list, dir string) bool {
	for _, p := range strings.Split(list, ";") {
		if strings.EqualFold(p, dir) {
			return true
		}
	}
	return false
}

func broadcastEnvChange() {
	env, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	const hwndBroadcast, wmSettingChange, smtoAbortIfHung = 0xffff, 0x001A, 0x0002
	var result uintptr
	_, _, _ = proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}
