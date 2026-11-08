package platform

import (
	"os"
	"os/exec"
	"runtime"
	"time"
)

const openWait = 5 * time.Second

func OpenURL(url string) error {
	// VS Code (web, remote, Codespaces) points BROWSER at a helper that opens links on the user's own computer
	if b := os.Getenv("BROWSER"); b != "" && start(exec.Command(b, url)) == nil {
		return nil
	}
	name, args := "xdg-open", []string{url}
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	}
	return start(exec.Command(name, args...))
}

func start(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	// xdg-open may wait on the browser it started; never kill that
	case <-time.After(openWait):
		return nil
	}
}
