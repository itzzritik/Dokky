package daemon

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"syscall"
	"unicode/utf16"
)

const createNoWindow = 0x08000000

func currentUser() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot find the current Windows user: %w", err)
	}
	return u.Username, nil
}

func schtasks(args ...string) error {
	cmd := exec.Command("schtasks", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return nil
}

func taskName() (string, error) {
	u, err := currentUser()
	if err != nil {
		return "", err
	}
	return windowsTaskName(u), nil
}

func RenderService(binPath string) string {
	u, _ := currentUser()
	return renderTaskXML(binPath, u)
}

// The task XML is not read back, so the task is always re-registered.
func ServiceCurrent(string) bool { return false }

// schtasks wants UTF-16LE with a BOM.
func utf16LEWithBOM(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 2, 2+2*len(units))
	out[0], out[1] = 0xFF, 0xFE
	for _, u := range units {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	return out
}

func InstallService(binPath string) error {
	name, err := taskName()
	if err != nil {
		return err
	}
	u, _ := currentUser()
	body := renderTaskXML(binPath, u)
	tmp, err := os.CreateTemp("", "dokky-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(utf16LEWithBOM(body)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return schtasks("/Create", "/TN", name, "/XML", tmp.Name(), "/F")
}

func StartService() error {
	name, err := taskName()
	if err != nil {
		return err
	}
	return schtasks("/Run", "/TN", name)
}

func StopService() error {
	name, err := taskName()
	if err != nil || !ServiceInstalled() {
		return err
	}
	_ = schtasks("/End", "/TN", name)
	return nil
}

func ServiceInstalled() bool {
	name, err := taskName()
	return err == nil && schtasks("/Query", "/TN", name) == nil
}

func RemoveService() error {
	name, err := taskName()
	if err != nil || !ServiceInstalled() {
		return err
	}
	_ = StopService()
	return schtasks("/Delete", "/TN", name, "/F")
}
