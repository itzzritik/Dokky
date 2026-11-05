package platform

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// installs WinFsp with winget, which asks Windows for permission; exec.ErrNotFound when winget is missing
func InstallWinFsp() error {
	out, err := exec.Command("winget", "install", "-e", "--id", "WinFsp.WinFsp",
		"--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity").CombinedOutput()
	if err != nil && !errors.Is(err, exec.ErrNotFound) {
		err = fmt.Errorf("%w: %s", err, lastLine(out))
	}
	return err
}

// winget redraws progress with \r; its last words say what went wrong
func lastLine(out []byte) string {
	lines := strings.FieldsFunc(string(out), func(r rune) bool { return r == '\n' || r == '\r' })
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}
