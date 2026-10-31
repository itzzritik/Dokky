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
	"github.com/stretchr/testify/require"
)

const gib = 1 << 30

type fakeDaemon struct {
	mu      sync.Mutex
	replies map[string]any
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
		{ID: "m1", Name: "media", State: "mounted"},
		{ID: "m2", Name: "docs", State: "error", Error: "connection refused"},
		{ID: "m3", Name: "old", State: "removed"},
	}})
	v := view(start(f))
	require.Contains(t, v, "mounted")
	require.NotContains(t, v, "unmounted")
	require.Contains(t, v, "error")
	require.Contains(t, v, "connection refused")
	require.Contains(t, v, "removed on another device")
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
