package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

var errNoWinFsp = errors.New("WinFsp isn't installed, and Windows needs it to show your drives. Install it with: winget install -e --id WinFsp.WinFsp")

// looks where cgofuse loads WinFsp from (by name, then the registry's InstallDir), so a missing one is a plain error instead of its panic
func CheckDriver() error {
	name := map[string]string{"arm64": "winfsp-a64.dll", "amd64": "winfsp-x64.dll", "386": "winfsp-x86.dll"}[runtime.GOARCH]
	if dll, err := syscall.LoadDLL(name); err == nil {
		_ = dll.Release()
		return nil
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\WinFsp`, registry.QUERY_VALUE|registry.WOW64_32KEY)
	if err != nil {
		return errNoWinFsp
	}
	defer k.Close()
	dir, _, err := k.GetStringValue("InstallDir")
	if err != nil {
		return errNoWinFsp
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", name)); err != nil {
		return fmt.Errorf("WinFsp in %s has no %s for this %s PC, update it with: winget install -e --id WinFsp.WinFsp", dir, name, runtime.GOARCH)
	}
	return nil
}
