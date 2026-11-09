//go:build e2e && !windows

package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/stretchr/testify/require"
)

const e2ePassword = "Orchard-Signal-Ferry-62"

type device struct {
	state string
}

type idReply struct {
	ID string `json:"id"`
}

func TestTwoDevicesSync(t *testing.T) {
	api, helper := startAPI(t)
	url, dir := startWebDAV(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644))
	t.Setenv("DOKKY_API_BASE", api)
	a, b := startDevice(t), startDevice(t)
	a.login(t, helper)
	b.login(t, helper)
	require.NotEmpty(t, a.keyID(t))
	require.Equal(t, a.keyID(t), b.keyID(t), "both devices use the account's key")

	var svc, mnt idReply
	a.call(t, "service.put", store.Service{Name: "media", Type: "webdav", Options: map[string]string{"url": url, "vendor": "other", "user": "me", "pass": "s3cret"}}, &svc)
	a.call(t, "mount.put", store.Mount{Name: "media", ServiceID: svc.ID, VFS: store.DefaultVFS()}, &mnt)
	var ms daemon.MountStatus
	a.call(t, "mount", map[string]string{"name": "media"}, &ms)
	require.Equal(t, "mounted", ms.State)
	mp := ms.MountPoint
	readable(t, mp, "hello.txt", "hello")

	b.call(t, "sync.now", nil, nil)
	got := b.only(t)
	require.Equal(t, mnt.ID, got.ID)
	require.Equal(t, "Media", got.Name, "names are saved capitalized")
	require.Equal(t, svc.ID, got.ServiceID)
	require.Equal(t, "unmounted", got.State)
	var info struct {
		Options    map[string]string `json:"options"`
		SecretsSet []string          `json:"secretsSet"`
	}
	b.call(t, "service.get", idReply{ID: svc.ID}, &info)
	require.Equal(t, url, info.Options["url"])
	require.Equal(t, []string{"pass", "user"}, info.SecretsSet)
	readable(t, mp, "hello.txt", "hello")

	b.call(t, "mount.delete", idReply{ID: mnt.ID}, nil)
	require.Empty(t, b.mounts(t))
	readable(t, mp, "hello.txt", "hello")

	// a drive removed on another device leaves this one too, once nothing on it is moving
	a.call(t, "sync.now", nil, nil)
	waitFor(t, func() bool { return len(a.mounts(t)) == 0 })
	require.False(t, isMounted(mp))
	var services []idReply
	a.call(t, "service.list", nil, &services)
	require.Equal(t, []idReply{svc}, services)
}

func startAPI(t *testing.T) (api, helper string) {
	t.Helper()
	missing := t.Skip
	if os.Getenv("CI") == "true" {
		missing = t.Fatal
	}
	if _, err := exec.LookPath("bun"); err != nil {
		missing("bun is not on PATH; this test runs the web API with bun")
	}
	web, err := filepath.Abs(filepath.Join("..", "..", "web"))
	require.NoError(t, err)
	if !exists(filepath.Join(web, "node_modules", ".bin", "next")) {
		missing("web deps are not installed; run bun install in web/")
	}
	cmd := exec.Command("bun", filepath.Join("test", "e2e-server.ts"))
	cmd.Dir = web
	cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 10 * time.Second
	var logs bytes.Buffer
	cmd.Stderr = &logs
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	ready := make(chan []byte, 1)
	go func() {
		defer close(ready)
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			// a fresh machine first prints mongodb-memory-server's download progress
			if line := sc.Bytes(); bytes.HasPrefix(line, []byte("{")) {
				ready <- bytes.Clone(line)
				break
			}
		}
		for sc.Scan() {
		}
	}()
	t.Cleanup(func() {
		exited := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(exited)
		}()
		_ = stdin.Close()
		select {
		case <-exited:
		case <-time.After(30 * time.Second):
			t.Error("e2e server did not stop within 30s, killing it")
		}
		// mongod and next share the group
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-exited
		if t.Failed() {
			t.Logf("e2e server output:\n%s", logs.String())
		}
	})

	var urls struct{ API, Helper string }
	select {
	case line, ok := <-ready:
		require.True(t, ok, "e2e server exited before it was ready")
		require.NoError(t, json.Unmarshal(line, &urls), string(line))
	case <-time.After(5 * time.Minute):
		t.Fatal("e2e server was not ready within 5 minutes")
	}
	return urls.API, urls.Helper
}

func startDevice(t *testing.T) device {
	t.Helper()
	home := daemonHome(t)
	// login.start opens the real verify URL in a browser
	bin := filepath.Join(home, ".local", "bin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	for _, name := range []string{"open", "xdg-open"} {
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755))
	}
	guardMount(t, engine.DefaultMountPoint(home, "media", runtime.GOOS))
	startDaemon(t)
	state, err := platform.StateDir()
	require.NoError(t, err)
	return device{state: state}
}

func (d device) call(t *testing.T, method string, params, result any) {
	t.Helper()
	require.NoError(t, ipcAt(d.state, method, params, result), method)
}

func (d device) login(t *testing.T, helper string) {
	t.Helper()
	var start struct {
		UserCode string `json:"userCode"`
	}
	d.call(t, "login.start", nil, &start)
	body, err := json.Marshal(map[string]string{"userCode": start.UserCode})
	require.NoError(t, err)
	res, err := http.Post(helper+"/approve", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	var poll struct {
		Status  string `json:"status"`
		Clashes []any  `json:"clashes"`
	}
	d.call(t, "login.poll", nil, &poll)
	if poll.Status == "vault" {
		// a device syncs only with a master password: the first one becomes the account's, the next device joins it
		var s api.State
		d.call(t, api.MState, nil, &s)
		switch s.Vault.Need {
		case api.NeedCreate:
			d.call(t, api.MVaultSetup, api.PasswordParams{Password: e2ePassword}, nil)
		case api.NeedJoin:
			d.call(t, api.MVaultJoin, api.PasswordParams{Password: e2ePassword}, nil)
		default:
			t.Fatalf("sign in waits for the vault but needs %q", s.Vault.Need)
		}
		d.call(t, "login.poll", nil, &poll)
	}
	require.Equal(t, "approved", poll.Status)
	require.Empty(t, poll.Clashes)
	var st daemon.StatusReply
	d.call(t, "status", nil, &st)
	require.True(t, st.LoggedIn)
	require.False(t, st.Pending)
	require.Equal(t, api.VaultUnlocked, st.Vault)
	require.Empty(t, st.SyncWaits)
}

func (d device) keyID(t *testing.T) string {
	t.Helper()
	var s api.State
	d.call(t, api.MState, nil, &s)
	return s.Vault.KeyID
}

func (d device) mounts(t *testing.T) []daemon.MountStatus {
	t.Helper()
	var st daemon.StatusReply
	d.call(t, "status", nil, &st)
	return st.Mounts
}

func (d device) only(t *testing.T) daemon.MountStatus {
	t.Helper()
	ms := d.mounts(t)
	require.Len(t, ms, 1)
	return ms[0]
}
