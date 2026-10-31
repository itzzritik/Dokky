package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/logx"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/lib/oauthutil"
	"github.com/rclone/rclone/librclone/librclone"
	"github.com/stretchr/testify/require"
)

func call(t *testing.T, d *Daemon, method string, params, out any) error {
	t.Helper()
	h, ok := d.handlers(nil)[method]
	require.True(t, ok, "no handler for %s", method)
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	res, err := h(context.Background(), raw)
	if err != nil || out == nil {
		return err
	}
	b, err := json.Marshal(res)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, out))
	return nil
}

func newEngineDaemon(t *testing.T, data store.Data) *Daemon {
	t.Helper()
	e, err := engine.Start(t.TempDir(), nil)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	d, _, _ := newTestDaemon(t, data)
	d.eng = e
	d.start()
	return d
}

func rcCall(t *testing.T, method string, in, out any) {
	t.Helper()
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	res, status := librclone.RPC(method, string(raw))
	require.Equal(t, http.StatusOK, status, res)
	require.NoError(t, json.Unmarshal([]byte(res), out))
}

func remotes(t *testing.T) []string {
	t.Helper()
	var out struct{ Remotes []string }
	rcCall(t, "config/listremotes", nil, &out)
	return out.Remotes
}

func section(t *testing.T, name string) map[string]string {
	t.Helper()
	var out map[string]string
	rcCall(t, "config/get", map[string]string{"name": name}, &out)
	return out
}

func savedDisk(t *testing.T) (*Daemon, string) {
	t.Helper()
	root := t.TempDir()
	return newEngineDaemon(t, store.Data{
		Services: []store.Service{{ID: "disk", Name: "disk", Type: "alias", Options: map[string]string{"remote": root}}},
		Local:    map[string]store.LocalMount{},
	}), root
}

func TestServicePutSplitsSecrets(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	var reply struct{ ID string }
	require.NoError(t, call(t, d, "service.put", store.Service{
		Name: "box", Type: "sftp",
		Options: map[string]string{"host": "nas.local", "port": "2022", "pass": "hunter2-pw"},
	}, &reply))
	require.NoError(t, uuid.Validate(reply.ID))

	saved := load(t, st).Services
	require.Len(t, saved, 2)
	require.Equal(t, reply.ID, saved[1].ID)
	require.Equal(t, map[string]string{"port": "2022"}, saved[1].Options)
	require.Equal(t, map[string]string{"host": "nas.local", "pass": "hunter2-pw"}, saved[1].Secrets)
	require.Equal(t, saved[1].Secrets, f.services[reply.ID].Secrets)
}

func TestServicePutUpdatesRedaction(t *testing.T) {
	d, _, _ := newTestDaemon(t, sample())
	ctx := context.Background()
	prevLevel, prevDefault := fs.GetConfig(ctx).LogLevel, slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(prevDefault)
		fs.GetConfig(ctx).LogLevel = prevLevel
		fs.SetLogger(prevDefault.Handler())
		logx.SetSecrets(nil)
	})
	path := filepath.Join(t.TempDir(), "dokky.log")
	d.log = logx.New(path)

	svc := store.Service{ID: "s2", Name: "box", Type: "sftp", Options: map[string]string{"host": "nas.local", "pass": "first-pass"}}
	require.NoError(t, call(t, d, "service.put", svc, nil))
	svc.Options["pass"] = "second-pass"
	require.NoError(t, call(t, d, "service.put", svc, nil))
	d.log.Error("login failed", "err", "ssh nas.local pass=second-pass")

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(b), "second-pass")
	require.NotContains(t, string(b), "nas.local")
	require.Contains(t, string(b), "***")
}

func TestBrowseReturnsDirsOnly(t *testing.T) {
	d, root := savedDisk(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "movies", "2024"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(root, "music"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644))

	var dirs []string
	require.NoError(t, call(t, d, "browse", map[string]string{"serviceId": "disk"}, &dirs))
	require.Equal(t, []string{"movies", "music"}, dirs)
	require.NoError(t, call(t, d, "browse", map[string]string{"serviceId": "disk", "path": "movies"}, &dirs))
	require.Equal(t, []string{"2024"}, dirs)
	require.Error(t, call(t, d, "browse", map[string]string{"serviceId": "nope"}, &dirs))
}

func TestConfigureAssignsOneID(t *testing.T) {
	d := newEngineDaemon(t, store.Data{Local: map[string]store.LocalMount{}})

	var first, second configureReply
	require.NoError(t, call(t, d, "service.configure", map[string]any{"type": "drive"}, &first))
	require.NoError(t, uuid.Validate(first.ID))
	require.NotNil(t, first.Step.Option)
	require.Equal(t, "config_shared_client_id", first.Step.Option.Name)

	require.NoError(t, call(t, d, "service.configure", map[string]any{
		"id": first.ID, "type": "drive", "state": first.Step.State, "result": "false",
	}, &second))
	require.Equal(t, first.ID, second.ID)
	require.NotNil(t, second.Step.Option)
	require.Equal(t, "client_id", second.Step.Option.Name)
}

func TestConfigureCancelStopsOAuth(t *testing.T) {
	d := newEngineDaemon(t, store.Data{Local: map[string]store.LocalMount{}})
	opened := make(chan string, 1)
	prev := oauthutil.OpenURL
	oauthutil.OpenURL = func(u string) error {
		select {
		case opened <- u:
		default:
		}
		return nil
	}
	t.Cleanup(func() { oauthutil.OpenURL = prev })

	h, ok := d.handlers(nil)["service.configure"]
	require.True(t, ok, "no handler for service.configure")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := h(ctx, json.RawMessage(`{"id":"half-made","type":"drive","options":{"client_id":"x","config_is_local":"true"}}`))
		done <- err
	}()

	select {
	case u := <-opened:
		require.Contains(t, u, "127.0.0.1:53682/auth")
	case err := <-done:
		t.Fatalf("configure returned before the browser step: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("rclone never asked to open the browser")
	}
	require.Contains(t, remotes(t), "half-made")

	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("configure kept waiting after its caller went away")
	}
	require.NotContains(t, remotes(t), "half-made")
	require.Eventually(t, func() bool {
		c, err := net.Dial("tcp", "127.0.0.1:53682")
		if err == nil {
			c.Close()
		}
		return err != nil
	}, 5*time.Second, 50*time.Millisecond)
}

func TestConfigureRefusesSavedID(t *testing.T) {
	d, root := savedDisk(t)
	before := section(t, "disk")
	require.Equal(t, root, before["remote"])

	err := call(t, d, "service.configure", map[string]any{
		"id": "disk", "type": "alias", "options": map[string]string{"remote": t.TempDir()},
	}, nil)
	require.ErrorContains(t, err, "disk")
	require.Equal(t, before, section(t, "disk"))
}

func TestConfigureErrorLeavesNoSection(t *testing.T) {
	d := newEngineDaemon(t, store.Data{Local: map[string]store.LocalMount{}})
	err := call(t, d, "service.configure", map[string]any{
		"id": "broken", "type": "alias", "options": map[string]string{"remote": "a\nb"},
	}, nil)
	require.Error(t, err)
	require.Empty(t, remotes(t))
}

func TestServiceTestLeavesConfigAlone(t *testing.T) {
	d, root := savedDisk(t)
	before := section(t, "disk")

	var reply testReply
	edited := store.Service{ID: "disk", Name: "disk", Type: "alias", Options: map[string]string{"remote": t.TempDir()}}
	require.NoError(t, call(t, d, "service.test", map[string]any{"service": edited}, &reply))
	require.Equal(t, testReply{OK: true}, reply)

	fresh := store.Service{Name: "new", Type: "alias", Options: map[string]string{"remote": root}}
	require.NoError(t, call(t, d, "service.test", map[string]any{"service": fresh}, &reply))
	require.Equal(t, testReply{OK: true}, reply)

	fresh.Options["remote"] = filepath.Join(root, "missing")
	require.NoError(t, call(t, d, "service.test", map[string]any{"service": fresh}, &reply))
	require.False(t, reply.OK)
	require.NotEmpty(t, reply.Error)

	require.Equal(t, []string{"disk"}, remotes(t))
	require.Equal(t, before, section(t, "disk"))
}
