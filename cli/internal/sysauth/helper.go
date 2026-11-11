package sysauth

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"

	"github.com/itzzritik/dokky/cli/internal/platform"
)

// the macOS Touch ID helper this dokky carries; empty in a build made without it, which asks through osascript
var helperBin []byte

var helperDir = platform.StateDir

// helperPath writes the helper beside dokky's state the first time it is needed, again whenever this dokky
// carries a different one, and returns where it is; "" when there is none or it could not be written
var helperPath = sync.OnceValue(installHelper)

func installHelper() string {
	if len(helperBin) == 0 {
		return ""
	}
	dir, err := helperDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(dir, "dokky-auth")
	if b, err := os.ReadFile(path); err == nil && bytes.Equal(b, helperBin) {
		return path
	}
	if err := os.MkdirAll(dir, 0o700); err != nil || writeExecutable(path, helperBin) != nil {
		return ""
	}
	return path
}

// written whole and renamed into place, so a prompt never runs half a helper
func writeExecutable(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o700); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
