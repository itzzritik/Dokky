package platform

import (
	"os/exec"
	"runtime"
	"time"
)

const openWait = 5 * time.Second

func OpenURL(url string) error {
	name, args := "xdg-open", []string{url}
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	}
	cmd := exec.Command(name, args...)
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
