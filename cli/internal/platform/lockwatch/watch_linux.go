package lockwatch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

type monitor struct {
	args []string
	bus  func() bool
}

// logind on the system bus for a session lock and a sleep, and the screensaver on the session bus: KDE and most
// desktops signal org.freedesktop.ScreenSaver, GNOME only its own
var monitors = []monitor{
	{[]string{"--system", "--dest", "org.freedesktop.login1"}, hasSystemBus},
	{[]string{"--session", "--dest", "org.freedesktop.ScreenSaver", "--object-path", "/org/freedesktop/ScreenSaver"}, hasSessionBus},
	{[]string{"--session", "--dest", "org.gnome.ScreenSaver", "--object-path", "/org/gnome/ScreenSaver"}, hasSessionBus},
}

func watchOS(ctx context.Context, onLock func()) {
	gdbus, err := exec.LookPath("gdbus")
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for _, m := range monitors {
		if !m.bus() {
			continue
		}
		wg.Go(func() {
			keep(ctx, func(ctx context.Context) {
				cmd := exec.CommandContext(ctx, gdbus, append([]string{"monitor"}, m.args...)...)
				// a daemon killed outright takes its monitors with it
				cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
				scan(cmd, lockLine, onLock)
			})
		})
	}
	wg.Wait()
}

// every logind session is watched, so another person's lock on a shared computer ends this session too, which
// is the safe way round
func lockLine(line string) bool {
	l := strings.ToLower(line)
	return strings.Contains(l, ".session.lock (") || strings.Contains(l, "'lockedhint': <true>") ||
		strings.Contains(l, ".prepareforsleep (true") || strings.Contains(l, ".activechanged (true")
}

func hasSystemBus() bool {
	if os.Getenv("DBUS_SYSTEM_BUS_ADDRESS") != "" {
		return true
	}
	_, err := os.Stat("/run/dbus/system_bus_socket")
	return err == nil
}

func hasSessionBus() bool {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "" {
		return true
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "bus"))
	return err == nil
}
