package sysauth

import (
	"context"
	"fmt"
	"strings"
)

// pkexec shows the desktop's polkit dialog; --disable-internal-agent stops it falling back to a terminal prompt
// nobody would see from a daemon
func polkitCommand(pkexec string) command {
	return command{path: pkexec, args: []string{"--disable-internal-agent", "/bin/true"}}
}

func hasDisplay() bool {
	return strings.TrimSpace(getenv("DISPLAY")) != "" || strings.TrimSpace(getenv("WAYLAND_DISPLAY")) != ""
}

func polkitAvailable() (string, bool) {
	if !hasDisplay() {
		return "", false
	}
	if _, err := lookPath("pkexec"); err != nil {
		return "", false
	}
	return "your system password", true
}

func polkitPrompt(ctx context.Context) error {
	if !hasDisplay() {
		return fmt.Errorf("%w: there is no desktop session", ErrUnavailable)
	}
	pkexec, err := lookPath("pkexec")
	if err != nil {
		return fmt.Errorf("%w: pkexec is not installed", ErrUnavailable)
	}
	res, err := run(ctx, polkitCommand(pkexec))
	return parsePolkit(res, err)
}

// pkexec exits 126 when the dialog is dismissed and 127 when it could not get authorization at all,
// like when no polkit agent runs in this session
func parsePolkit(res result, err error) error {
	switch {
	case err != nil && stopped(err):
		return ErrCancelled
	case err != nil:
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	switch res.code {
	case 0:
		return nil
	case 126:
		return ErrCancelled
	case 127:
		return fmt.Errorf("%w: polkit could not confirm it is you", ErrUnavailable)
	}
	return fmt.Errorf("could not confirm it is you with polkit (exit %d)", res.code)
}
