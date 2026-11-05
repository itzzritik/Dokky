//go:build !windows

package platform

import "errors"

func InstallWinFsp() error { return errors.New("WinFsp is only for Windows") }
