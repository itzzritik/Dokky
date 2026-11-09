package daemon

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"

	"github.com/itzzritik/dokky/cli/internal/platform"
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

// schtasks can't give the XML back in a form worth parsing, so a hash of what was registered stands in for it
const taskMarker = "task.sha256"

func taskHash(binPath string) string {
	sum := sha256.Sum256([]byte(RenderService(binPath)))
	return hex.EncodeToString(sum[:])
}

func markerPath() (string, error) {
	state, err := platform.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, taskMarker), nil
}

// re-registering a task while its daemon runs can stop it and drop every drive, so it only happens when the task really changed
func ServiceCurrent(binPath string) bool {
	path, err := markerPath()
	if err != nil {
		return false
	}
	got, err := os.ReadFile(path)
	return err == nil && strings.TrimSpace(string(got)) == taskHash(binPath) && ServiceInstalled()
}

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
	if err := schtasks("/Create", "/TN", name, "/XML", tmp.Name(), "/F"); err != nil {
		return err
	}
	if path, err := markerPath(); err == nil {
		_ = os.WriteFile(path, []byte(taskHash(binPath)), 0o600)
	}
	return nil
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

// leaves the running daemon alone, so StopService after it is a no-op
func unregisterService() error {
	name, err := taskName()
	if err != nil || !ServiceInstalled() {
		return err
	}
	if path, err := markerPath(); err == nil {
		_ = os.Remove(path)
	}
	return schtasks("/Delete", "/TN", name, "/F")
}

// Windows can't delete a running exe or an open file, so a helper retries until the daemon has exited
func deleteAfterExit(left []string, state string) error {
	var del, rd string
	for _, p := range left {
		fi, err := os.Lstat(p)
		switch {
		case err != nil:
		case fi.Mode().IsRegular():
			del += fmt.Sprintf(` "%s"`, p)
		case fi.Mode().IsDir():
			rd += fmt.Sprintf(` & rmdir /s /q "%s" 2>nul`, p)
		}
	}
	if del == "" && rd == "" {
		return nil
	}
	if del != "" {
		del = " & del /f /q" + del + " 2>nul"
	}
	line := fmt.Sprintf(`(for /l %%i in (1,1,15) do @(ping -n 2 127.0.0.1 >nul%s%s)) & rmdir "%s" 2>nul`, del, rd, state)
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | syscall.CREATE_NEW_PROCESS_GROUP,
		CmdLine:       "cmd.exe /d /c " + line,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func refreshService(string) (bool, error) { return false, nil }
