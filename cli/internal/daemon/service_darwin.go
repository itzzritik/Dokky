package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

func domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

func launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return nil
}

func loaded() bool {
	return exec.Command("launchctl", "print", domain()+"/"+launchdLabel).Run() == nil
}

func RenderService(binPath string) string { return renderLaunchAgent(binPath) }

func InstallService(binPath string) error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(RenderService(binPath)), 0o644); err != nil {
		return err
	}
	if !loaded() {
		return nil
	}
	if err := launchctl("bootout", domain()+"/"+launchdLabel); err != nil {
		return err
	}
	for i := 0; i < 50 && loaded(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func ServiceCurrent(binPath string) bool {
	path, err := plistPath()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(path)
	return err == nil && string(b) == RenderService(binPath)
}

func StartService() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if loaded() {
		return launchctl("kickstart", domain()+"/"+launchdLabel)
	}
	return launchctl("bootstrap", domain(), path)
}

func StopService() error {
	if !loaded() {
		return nil
	}
	return launchctl("bootout", domain()+"/"+launchdLabel)
}

func ServiceInstalled() bool {
	path, err := plistPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func unregisterService() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func deleteAfterExit([]string, string) error { return nil }
