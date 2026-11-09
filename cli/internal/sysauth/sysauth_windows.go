package sysauth

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000

func available(ctx context.Context) (string, bool) {
	shell, err := windowsPowerShell()
	if err != nil {
		return "", false
	}
	return helloAvailable(ctx, shell)
}

func prompt(ctx context.Context, reason string) error {
	shell, err := windowsPowerShell()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return helloPrompt(ctx, shell, reason)
}

func windowsPowerShell() (string, error) {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return "", err
	}
	return powerShellPath(dir), nil
}

// no console window flashes up for the helper; the Hello dialog belongs to the system, not to it
func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// the daemon starts without foreground rights, so its prompt would open behind the active app
func started(c command, _ *exec.Cmd) func() {
	if !c.raise {
		return func() {}
	}
	find := helloPromptFinder()
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		raiseWhenShown(stop, find)
	}()
	return func() {
		close(stop)
		<-done
	}
}
