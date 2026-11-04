package tui

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
	"github.com/stretchr/testify/require"
)

const gib = 1 << 30

type fakeDaemon struct {
	mu      sync.Mutex
	replies map[string]any
	errs    map[string]error
	calls   []string
}

func newFake(st daemon.StatusReply) *fakeDaemon {
	return &fakeDaemon{replies: map[string]any{
		"status":       st,
		"service.list": []serviceItem{{ID: "s1", Name: "nas", Type: "sftp"}, {ID: "s2", Name: "box", Type: "webdav"}},
		"browse":       []string{"Media"},
	}}
}

func (f *fakeDaemon) Call(_ context.Context, method string, params, result any) error {
	raw, _ := json.Marshal(params)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+" "+string(raw))
	if err := f.errs[method]; err != nil {
		return err
	}
	if r, ok := f.replies[method]; ok && result != nil {
		b, _ := json.Marshal(r)
		return json.Unmarshal(b, result)
	}
	return nil
}

func (f *fakeDaemon) Events(context.Context) (<-chan ipc.Event, error) {
	return nil, errors.New("no events in tests")
}

func (f *fakeDaemon) called(method string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if m, params, _ := strings.Cut(c, " "); m == method {
			out = append(out, params)
		}
	}
	return out
}

// run feeds every message a command produces back into the model; ticks and cursor blinks never finish in time and are dropped
func run(m tea.Model, cmd tea.Cmd) tea.Model {
	queue := []tea.Cmd{cmd}
	for i := 0; len(queue) > 0 && i < 200; i++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(100 * time.Millisecond):
			continue
		}
		// tea.Batch and huh's tea.Sequence both deliver a []tea.Cmd
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
			for j := range v.Len() {
				queue = append(queue, v.Index(j).Interface().(tea.Cmd))
			}
			continue
		}
		var next tea.Cmd
		m, next = m.Update(msg)
		queue = append(queue, next)
	}
	return m
}

func start(f *fakeDaemon) tea.Model {
	a := newApp(f)
	m, _ := a.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return run(m, a.Init())
}

func press(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "ctrl+u":
			msg = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
		default:
			for _, r := range k {
				var next tea.Cmd
				m, next = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
				m = run(m, next)
			}
			continue
		}
		var next tea.Cmd
		m, next = m.Update(msg)
		m = run(m, next)
	}
	return m
}

func view(m tea.Model) string { return ansi.Strip(m.View().Content) }

func TestHomeEnterMountsUnmounted(t *testing.T) {
	f := newFake(daemon.StatusReply{Mounts: []daemon.MountStatus{{ID: "m1", Name: "nas", State: "unmounted"}}})
	press(start(f), "enter")
	require.Equal(t, []string{`{"name":"nas"}`}, f.called("mount"))
	require.Empty(t, f.called("unmount"))
}

func TestStatusShowsWords(t *testing.T) {
	f := newFake(daemon.StatusReply{Mounts: []daemon.MountStatus{
		{ID: "m1", Name: "media", State: "mounted", RemountToApply: true},
		{ID: "m2", Name: "docs", State: "error", Error: "connection refused"},
		{ID: "m3", Name: "old", State: "removed"},
	}})
	v := view(start(f))
	require.Contains(t, v, "mounted")
	require.NotContains(t, v, "unmounted")
	require.Contains(t, v, "error")
	require.Contains(t, v, "connection refused")
	require.Contains(t, v, "removed on another device")
	require.Equal(t, 1, strings.Count(v, "remount to apply"))
}

func TestAddMountRejectsBadName(t *testing.T) {
	f := newFake(daemon.StatusReply{})
	m := press(start(f), "a")
	require.Contains(t, view(m), "box")
	m = press(m, "enter")
	require.Equal(t, []string{`{"serviceId":"s1","path":""}`}, f.called("browse"))
	require.Contains(t, view(m), "Media")

	m = press(m, "down", "enter")
	require.Equal(t, `{"serviceId":"s1","path":"Media"}`, f.called("browse")[1])
	m = press(m, "enter", "ctrl+u", "Bad Name", "enter")
	require.Contains(t, view(m), "lowercase letters")
	require.Empty(t, f.called("mount.put"))

	m = press(m, "ctrl+u", "media", "enter")
	puts := f.called("mount.put")
	require.Len(t, puts, 1)
	require.Contains(t, puts[0], `"name":"media","serviceId":"s1","remotePath":"Media"`)
	require.Contains(t, puts[0], `"cacheMaxAge":"5m"`)
	require.Contains(t, view(m), "Mount media now?")
	press(m, "y")
	require.Equal(t, []string{`{"name":"media"}`}, f.called("mount"))
}

func TestDeleteServiceListsMounts(t *testing.T) {
	f := newFake(daemon.StatusReply{Mounts: []daemon.MountStatus{
		{ID: "m1", Name: "movies", ServiceID: "s1", State: "mounted"},
		{ID: "m2", Name: "photos", ServiceID: "s1", State: "unmounted"},
		{ID: "m3", Name: "backups", ServiceID: "s2", State: "unmounted"},
	}})
	m := press(start(f), "tab", "d")
	v := view(m)
	require.Contains(t, v, "Delete nas?")
	require.Contains(t, v, "movies")
	require.Contains(t, v, "photos")
	require.NotContains(t, v, "backups")
	require.Empty(t, f.called("service.delete"))

	press(m, "y")
	require.Equal(t, []string{`{"id":"s1"}`}, f.called("service.delete"))
}

func TestLowSpaceWarnsOnce(t *testing.T) {
	low := daemon.StatusReply{FreeBytes: 5 * gib, MinFreeBytes: 10 * gib}
	m := start(newFake(low))
	require.Equal(t, 1, strings.Count(view(m), "Low disk space"))
	require.Contains(t, view(m), "Free space: 5 GiB")

	m = press(m, "down")
	require.NotContains(t, view(m), "Low disk space")
	m, _ = m.Update(statusMsg(low))
	require.NotContains(t, view(m), "Low disk space")
	require.Contains(t, view(m), "Free space: 5 GiB")
}

func TestEditServicePrefillsAndKeepsSecrets(t *testing.T) {
	f := newFake(daemon.StatusReply{})
	f.replies["providers"] = []engine.Provider{{Type: "sftp", Description: "SSH/SFTP", Options: []engine.Option{
		{Name: "user", Type: "string", Required: true, Secret: true},
		{Name: "port", Type: "int", Default: "22"},
		{Name: "pass", Type: "string", Secret: true},
		{Name: "shell_type", Type: "string", Advanced: true},
	}}}
	f.replies["service.get"] = map[string]any{"id": "s1", "name": "nas", "type": "sftp", "version": 3,
		"options": map[string]string{"port": "2022", "shell_type": "unix"}, "secretsSet": []string{"pass", "user"}}
	f.replies["service.configure"] = map[string]any{"id": "tmp", "step": engine.Step{State: "s1",
		Option: &engine.Option{Name: "config_is_local", Help: "Use web browser?", Type: "bool", Default: "true", Exclusive: true}}}
	f.replies["service.test"] = map[string]any{"ok": false, "error": "login refused"}

	m := press(start(f), "tab", "e")
	require.Equal(t, []string{`{"id":"s1"}`}, f.called("service.get"))
	v := view(m)
	require.Contains(t, v, "> nas")
	require.Contains(t, v, "> 2022")
	require.Equal(t, 2, strings.Count(v, "Saved. Leave blank to keep it."))

	m = press(m, "enter", "enter", "enter", "enter", "enter")
	require.Contains(t, view(m), "Use web browser?")
	f.replies["service.configure"] = map[string]any{"id": "tmp", "step": engine.Step{Done: true,
		Service: map[string]string{"user": "", "pass": "", "port": "2022", "shell_type": "unix"}}}
	m = press(m, "enter")
	require.Contains(t, view(m), "login refused")
	require.Empty(t, f.called("service.put"))

	f.replies["service.test"] = map[string]any{"ok": true}
	press(m, "enter", "enter", "enter", "enter", "enter", "enter")
	cfg := f.called("service.configure")
	require.Len(t, cfg, 3)
	require.Equal(t, `{"editOf":"s1","id":"","options":{"port":"2022","shell_type":"unix"},"result":"","state":"","type":"sftp"}`, cfg[0])
	require.Equal(t, `{"editOf":"s1","id":"tmp","options":null,"result":"true","state":"s1","type":"sftp"}`, cfg[1])
	require.Contains(t, f.called("service.test")[0], `"editOf":"s1"`)
	require.Equal(t, []string{`{"id":"s1","name":"nas","type":"sftp","options":{"pass":"","port":"2022","shell_type":"unix","user":""},"secrets":null,"version":3,"updatedAt":"0001-01-01T00:00:00Z","editOf":"s1"}`}, f.called("service.put"))
}

func TestDevicesTabRevokesAnotherDevice(t *testing.T) {
	f := newFake(daemon.StatusReply{LoggedIn: true})
	seen := time.Date(2026, 10, 31, 9, 30, 0, 0, time.UTC)
	f.replies["devices.list"] = []dsync.Device{
		{ID: "d1", Name: "macbook", OS: "darwin", Version: "1.2.0", LastSeenAt: seen, Current: true},
		{ID: "d2", Name: "office-pc", OS: "windows", Version: "1.1.0"},
	}
	m := start(f)
	require.Len(t, f.called("sync.now"), 1, "opening the TUI pulls")

	m = press(m, "tab", "tab", "tab", "tab")
	v := view(m)
	require.Contains(t, v, "macbook")
	require.Contains(t, v, "this device")
	require.Contains(t, v, "office-pc")
	require.Contains(t, v, "windows")
	require.Contains(t, v, "1.1.0")
	require.Contains(t, v, seen.Local().Format(lastSeenLayout))
	require.Contains(t, v, "never")

	m = press(m, "r")
	require.Contains(t, view(m), "dokky logout")
	require.NotContains(t, view(m), "will be wiped")

	m = press(m, "down", "r")
	require.Contains(t, view(m), "Revoke office-pc?")
	require.Contains(t, view(m), "This device will be wiped, including any uploads it has not sent yet.")
	m = press(m, "n")
	require.NotContains(t, view(m), "will be wiped")
	require.Empty(t, f.called("devices.revoke"))

	press(m, "r", "y")
	require.Equal(t, []string{`{"id":"d2"}`}, f.called("devices.revoke"))
	require.Len(t, f.called("devices.list"), 2, "the list reloads after a revoke")
}

func TestDevicesTabNeedsLogin(t *testing.T) {
	f := newFake(daemon.StatusReply{})
	m := press(start(f), "tab", "tab", "tab", "tab")
	require.Contains(t, view(m), "Log in to see devices.")
	require.Empty(t, f.called("devices.list"))
}

func TestServiceSaveConflictEndsFlow(t *testing.T) {
	f := newFake(daemon.StatusReply{})
	f.replies["providers"] = []engine.Provider{{Type: "sftp", Description: "SSH/SFTP", Options: []engine.Option{{Name: "host", Type: "string", Required: true}}}}
	f.replies["service.get"] = map[string]any{"id": "s1", "name": "nas", "type": "sftp", "version": 1, "options": map[string]string{"host": "h"}, "secretsSet": []string{}}
	f.replies["service.configure"] = map[string]any{"id": "tmp", "step": engine.Step{Done: true, Service: map[string]string{"host": "h"}}}
	f.replies["service.test"] = map[string]any{"ok": true}
	f.errs = map[string]error{"service.put": &ipc.Error{Code: "conflict", Message: `{"id":"s1","name":"nas","options":{"host":"moved"},"version":2}`}}

	m := press(start(f), "tab", "e", "enter", "enter")
	require.Len(t, f.called("service.put"), 1)
	v := view(m)
	require.Contains(t, v, "saved on another device first")
	require.Contains(t, v, `"host":"moved"`)
	require.NotContains(t, v, "try again")
	require.Len(t, f.called("sync.now"), 2, "the newer copy is pulled right away")
	require.Len(t, f.called("service.list"), 3, "on open, on the tab and after the conflict")
}

func TestFeaturedFirst(t *testing.T) {
	var ps []engine.Provider
	for _, ty := range []string{"b2", "mega", "sftp", "alias", "drive", "webdav", "smb", "dropbox", "onedrive"} {
		ps = append(ps, engine.Provider{Type: ty, Description: ty + " desc"})
	}
	var got []string
	for _, p := range featuredFirst(ps) {
		got = append(got, p.Type)
	}
	require.Equal(t, append(append([]string{}, engine.Featured...), "b2", "alias"), got)

	f := newFake(daemon.StatusReply{})
	f.replies["providers"] = ps
	m := press(start(f), "tab", "a")
	v := view(m)
	last := -1
	for _, ty := range engine.Featured {
		i := strings.Index(v, "("+ty+")")
		require.Greater(t, i, last, ty)
		last = i
		require.Contains(t, v[i:], ": ")
	}
	require.Contains(t, v, "Files in your Google account")
	m = press(m, "/", "b2")
	v = view(m)
	require.Contains(t, v, "(b2)")
	require.NotContains(t, v, "(smb)")
}

func TestDriveClientIDIsAdvancedWithOwnClientHelp(t *testing.T) {
	f := newFake(daemon.StatusReply{})
	f.replies["providers"] = []engine.Provider{{Type: "drive", Description: "Google Drive", Options: []engine.Option{
		{Name: "client_id", Help: "Google Application Client Id", Type: "string"},
		{Name: "client_secret", Help: "OAuth Client Secret.", Type: "string", Secret: true},
	}}}
	m := press(start(f), "tab", "a", "enter")
	v := view(m)
	require.NotContains(t, v, "client id")
	require.Contains(t, v, "Show advanced options?")
	require.Contains(t, ownClientHelp, "being retired during 2026")
	require.Contains(t, ownClientHelp, "https://rclone.org/drive/#making-your-own-client-id")
}

func TestHomeDeleteMountConfirms(t *testing.T) {
	f := newFake(daemon.StatusReply{Mounts: []daemon.MountStatus{
		{ID: "m1", Name: "media", State: "mounted", UploadsQueued: 2, UploadsInProgress: 1},
		{ID: "m2", Name: "docs", State: "unmounted"},
	}})
	m := press(start(f), "d")
	v := view(m)
	require.Contains(t, v, "Delete media?")
	require.Contains(t, v, "It is mounted, so it is unmounted first.")
	require.Contains(t, v, "3 uploads have not finished")
	m = press(m, "n")
	require.NotContains(t, view(m), "Delete media?")
	require.Empty(t, f.called("mount.delete"))

	m = press(m, "down", "d")
	v = view(m)
	require.Contains(t, v, "Delete docs?")
	require.NotContains(t, v, "unmounted first")
	require.NotContains(t, v, "have not finished")
	statuses := len(f.called("status"))
	m = press(m, "y")
	require.Equal(t, []string{`{"id":"m2"}`}, f.called("mount.delete"))
	require.Greater(t, len(f.called("status")), statuses, "home refreshes after a delete")
	require.NotContains(t, view(m), "Delete docs?")
}

func TestHomeDeleteMountWarnsUnsent(t *testing.T) {
	f := newFake(daemon.StatusReply{Mounts: []daemon.MountStatus{{ID: "m2", Name: "docs", State: "unmounted"}}})
	f.errs = map[string]error{"mount.delete": &ipc.Error{Code: "unsent", Message: "4 files on docs are not uploaded yet"}}
	m := press(start(f), "d", "y")
	v := view(m)
	require.Contains(t, v, "Delete docs anyway?")
	require.Contains(t, v, "4 files on docs are not uploaded yet")

	m = press(m, "n")
	require.NotContains(t, view(m), "anyway")
	require.Len(t, f.called("mount.delete"), 1)

	m = press(m, "d", "y")
	f.mu.Lock()
	f.errs = nil
	f.mu.Unlock()
	press(m, "y")
	require.Equal(t, []string{`{"id":"m2"}`, `{"id":"m2"}`, `{"force":true,"id":"m2"}`}, f.called("mount.delete"))
}

func TestSettingsChangesCacheMaxAge(t *testing.T) {
	f := newFake(daemon.StatusReply{Mounts: []daemon.MountStatus{
		{ID: "m1", Name: "media", State: "mounted", CacheMaxAge: "5m", RemountToApply: true},
		{ID: "m2", Name: "docs", State: "unmounted", CacheMaxAge: "1h"},
	}})
	f.replies["mount.get"] = store.Mount{ID: "m1", Name: "media", ServiceID: "s1", RemotePath: "Media", VFS: store.DefaultVFS(), Version: 4}
	m := press(start(f), "tab", "tab", "tab")
	v := view(m)
	require.Contains(t, v, "Cache max age")
	require.Contains(t, v, "5m (remount to apply)")
	require.Contains(t, v, "1h")

	m = press(m, "down", "down", "enter")
	v = view(m)
	require.Contains(t, v, "Cache max age for media")
	for _, age := range engine.CacheMaxAges {
		require.Contains(t, v, age)
	}
	m = press(m, "esc")
	require.NotContains(t, view(m), "Cache max age for media")
	require.Empty(t, f.called("mount.put"))

	press(m, "enter", "down", "down", "enter")
	require.Equal(t, []string{`{"id":"m1"}`}, f.called("mount.get"))
	puts := f.called("mount.put")
	require.Len(t, puts, 1)
	require.Contains(t, puts[0], `"id":"m1","name":"media","serviceId":"s1","remotePath":"Media"`)
	require.Contains(t, puts[0], `"cacheMaxAge":"1h"`)
	require.Contains(t, puts[0], `"version":4`)
}

func TestSettingsCacheMaxAgeConflictPulls(t *testing.T) {
	f := newFake(daemon.StatusReply{Mounts: []daemon.MountStatus{{ID: "m1", Name: "media", State: "unmounted", CacheMaxAge: "5m"}}})
	f.replies["mount.get"] = store.Mount{ID: "m1", Name: "media", ServiceID: "s1", VFS: store.DefaultVFS(), Version: 1}
	f.errs = map[string]error{"mount.put": &ipc.Error{Code: "conflict", Message: `{"id":"m1"}`}}
	m := press(start(f), "tab", "tab", "tab", "down", "enter", "down", "enter")
	require.Len(t, f.called("mount.put"), 1)
	require.Contains(t, view(m), "media was changed on another device first")
	require.Len(t, f.called("sync.now"), 2, "the newer copy is pulled right away")
}
