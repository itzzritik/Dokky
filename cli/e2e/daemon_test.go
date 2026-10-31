//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/stretchr/testify/require"
)

type daemonProc struct {
	cmd  *exec.Cmd
	out  bytes.Buffer
	done chan struct{}
}

func daemonHome(t *testing.T) string {
	t.Helper()
	// short socket path for macOS
	root := "/tmp"
	if runtime.GOOS == "windows" {
		root = ""
	}
	home, err := os.MkdirTemp(root, "dk")
	require.NoError(t, err)
	home, err = filepath.EvalSymlinks(home)
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(home) })
	for k, v := range map[string]string{
		"HOME":            home,
		"USERPROFILE":     home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_CACHE_HOME":  filepath.Join(home, ".cache"),
		"LocalAppData":    filepath.Join(home, "AppData", "Local"),
		"PATH":            filepath.Join(home, ".local", "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
		"DOKKY_KEYRING":   "file",
	} {
		t.Setenv(k, v)
	}
	return home
}

func startDaemon(t *testing.T) *daemonProc {
	t.Helper()
	p := &daemonProc{cmd: exec.Command(dokkyBin, "daemon"), done: make(chan struct{})}
	p.cmd.Stdout, p.cmd.Stderr = &p.out, &p.out
	require.NoError(t, p.cmd.Start())
	go func() {
		_ = p.cmd.Wait()
		close(p.done)
	}()
	t.Cleanup(func() {
		p.stop(t)
		if t.Failed() {
			state, _ := platform.StateDir()
			logs, _ := os.ReadFile(filepath.Join(state, "dokky.log"))
			t.Logf("daemon output:\n%s\ndokky.log:\n%s", p.out.String(), logs)
		}
	})
	deadline := time.Now().Add(time.Minute)
	for {
		if _, err := daemonStatus(); err == nil {
			return p
		}
		select {
		case <-p.done:
			t.Fatalf("daemon exited early:\n%s", p.out.String())
		case <-time.After(200 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon socket never came up")
		}
	}
}

func (p *daemonProc) stop(t *testing.T) int {
	t.Helper()
	select {
	case <-p.done:
		return p.cmd.ProcessState.ExitCode()
	default:
	}
	// no SIGTERM on Windows
	if runtime.GOOS == "windows" {
		_ = ipcCall("daemon.restart", nil, nil)
	} else {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-p.done:
	case <-time.After(time.Minute):
		t.Error("daemon did not stop within a minute, killing it")
		_ = p.cmd.Process.Kill()
		<-p.done
	}
	return p.cmd.ProcessState.ExitCode()
}

func (p *daemonProc) kill(t *testing.T) {
	t.Helper()
	require.NoError(t, p.cmd.Process.Kill())
	<-p.done
}

func ipcCall(method string, params, result any) error {
	state, err := platform.StateDir()
	if err != nil {
		return err
	}
	return ipcAt(state, method, params, result)
}

func ipcAt(state, method string, params, result any) error {
	c, err := ipc.Connect(state)
	if err != nil {
		return err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return c.Call(ctx, method, params, result)
}

func daemonStatus() (daemon.StatusReply, error) {
	var st daemon.StatusReply
	err := ipcCall("status", nil, &st)
	return st, err
}

func dokky(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, dokkyBin, args...).CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

// register before startDaemon so it runs after the daemon stops
func guardMount(t *testing.T, point string) {
	t.Helper()
	if d, ok := t.Deadline(); ok {
		w := time.AfterFunc(time.Until(d)-time.Minute, func() { forceUnmount(point) })
		t.Cleanup(func() { w.Stop() })
	}
	t.Cleanup(func() {
		if isMounted(point) {
			t.Errorf("%s still mounted after the test, forcing it off", point)
			forceUnmount(point)
		}
	})
}

func addWebDAVMount(t *testing.T, url, name string) {
	t.Helper()
	svc := store.Service{ID: "svc-" + name, Name: name, Type: "webdav", Options: map[string]string{"url": url, "vendor": "other"}}
	require.NoError(t, ipcCall("service.put", svc, nil))
	require.NoError(t, ipcCall("mount.put", store.Mount{ID: "m-" + name, Name: name, ServiceID: svc.ID, VFS: store.DefaultVFS()}, nil))
}

func mountedAt(t *testing.T, name string) string {
	t.Helper()
	var point string
	require.Eventually(t, func() bool {
		st, err := daemonStatus()
		if err != nil {
			return false
		}
		for _, m := range st.Mounts {
			if m.Name == name && m.State == "mounted" {
				point = m.MountPoint
				return true
			}
		}
		return false
	}, time.Minute, 200*time.Millisecond)
	if runtime.GOOS == "windows" {
		return point + `\`
	}
	return point
}

func readable(t *testing.T, mp, name, want string) {
	t.Helper()
	within(t, 15*time.Second, mp, func() error {
		got, err := os.ReadFile(filepath.Join(mp, name))
		if err == nil && string(got) != want {
			err = fmt.Errorf("read %q, want %q", got, want)
		}
		return err
	})
}

func setupDaemonMount(t *testing.T) (point string, d *daemonProc) {
	t.Helper()
	url, dir := startWebDAV(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644))
	home := daemonHome(t)
	point = engine.DefaultMountPoint(home, "media", runtime.GOOS)
	guardMount(t, point)
	d = startDaemon(t)
	addWebDAVMount(t, url, "media")
	require.Contains(t, dokky(t, "mount", "media"), "media")
	readable(t, mountedAt(t, "media"), "hello.txt", "hello")
	return point, d
}

func TestRemountAfterDaemonRestart(t *testing.T) {
	point, d := setupDaemonMount(t)

	want := 0
	if runtime.GOOS == "windows" {
		want = 75
	}
	require.Equal(t, want, d.stop(t))
	require.False(t, isMounted(point))

	startDaemon(t)
	readable(t, mountedAt(t, "media"), "hello.txt", "hello")
	require.Regexp(t, `media\s+mounted`, dokky(t, "status"))
}

func TestStartupClearsStaleMount(t *testing.T) {
	point, d := setupDaemonMount(t)

	d.kill(t)
	if runtime.GOOS != "windows" {
		require.True(t, isMounted(point), "SIGKILL should leave the OS mount behind")
	}

	startDaemon(t)
	readable(t, mountedAt(t, "media"), "hello.txt", "hello")
}

func TestMountUnmountMountAgain(t *testing.T) {
	point, _ := setupDaemonMount(t)

	dokky(t, "unmount", "media")
	require.False(t, isMounted(point))
	require.NoDirExists(t, point)

	dokky(t, "mount", "media")
	mp := mountedAt(t, "media")
	// time for rclone's second unmount to knock the new mount off
	time.Sleep(2 * time.Second)
	readable(t, mp, "hello.txt", "hello")
	require.Regexp(t, `media\s+mounted`, dokky(t, "status"))
}
