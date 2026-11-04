package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func unitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", systemdUnit), nil
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return nil
}

func RenderService(binPath string) string { return renderSystemdUnit(binPath) }

func InstallService(binPath string) error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(RenderService(binPath)), 0o644); err != nil {
		return err
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	return systemctl("enable", "--now", systemdUnit)
}

func ServiceCurrent(binPath string) bool {
	path, err := unitPath()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(path)
	return err == nil && string(b) == RenderService(binPath)
}

func StartService() error { return systemctl("start", systemdUnit) }

func StopService() error { return systemctl("stop", systemdUnit) }

func ServiceInstalled() bool {
	path, err := unitPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func unregisterService() error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	if !ServiceInstalled() {
		return nil
	}
	if err := systemctl("disable", systemdUnit); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return systemctl("daemon-reload")
}

func deleteAfterExit([]string, string) error { return nil }

func refreshService(bin string) (bool, error) {
	path, err := unitPath()
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(RenderService(bin)), 0o644); err != nil {
		return false, err
	}
	return true, systemctl("daemon-reload")
}
