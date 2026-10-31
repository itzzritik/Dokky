package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/operations"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	fs.GetConfig(context.Background()).LogLevel = fs.LogLevelCritical
	os.Exit(m.Run())
}

func startEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := Start(t.TempDir(), nil)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	return e
}

func TestStartStop(t *testing.T) {
	dir := t.TempDir()
	e, err := Start(dir, nil)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	require.Same(t, e.mem, config.Data())
	require.Equal(t, filepath.Join(dir, "rclone.conf"), config.GetConfigPath())
	require.Equal(t, 4, fs.GetConfig(context.Background()).Transfers)

	_, err = Start(t.TempDir(), nil)
	require.ErrorContains(t, err, "already running")

	require.Error(t, e.PutService(store.Service{ID: "x", Type: "no-such-backend"}))
	require.NoError(t, e.PutService(store.Service{ID: "svc1", Type: "alias", Options: map[string]string{"remote": dir}}))
	config.SaveConfig()

	stats, err := e.Stats()
	require.NoError(t, err)
	require.Empty(t, stats)
	_, err = e.TotalBytes()
	require.NoError(t, err)

	e.Stop()
	require.NoFileExists(t, filepath.Join(dir, "rclone.conf"))

	e2, err := Start(t.TempDir(), nil)
	require.NoError(t, err)
	e2.Stop()
}

func TestConfigStepRevealsPasswords(t *testing.T) {
	e := startEngine(t)
	// a plain password that also parses as an obscured value
	pass := obscure.MustObscure("inner")
	step, err := e.ConfigStep(t.Context(), "svc1", "sftp", map[string]string{"host": "nas.local", "pass": pass}, "", "")
	require.NoError(t, err)
	require.True(t, step.Done)
	require.Equal(t, map[string]string{"host": "nas.local", "pass": pass}, step.Service)
	stored, _ := e.mem.GetValue("svc1", "pass")
	require.Equal(t, pass, obscure.MustReveal(stored))
}

func TestRemoteUsesCurrentConfigAndTimesOut(t *testing.T) {
	e := startEngine(t)
	dir := t.TempDir()
	s := store.Service{
		ID: "svc1", Name: "vault", Type: "crypt",
		Options: map[string]string{"remote": dir},
		Secrets: map[string]string{"password": "hunter2"},
	}
	require.NoError(t, e.TestRemote(s))
	s.Options = map[string]string{"remote": filepath.Join(dir, "missing")}
	require.Error(t, e.TestRemote(s))

	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(hang) })
	old := testRemoteTimeout
	testRemoteTimeout = time.Second
	t.Cleanup(func() { testRemoteTimeout = old })

	start := time.Now()
	err := e.TestRemote(store.Service{ID: "svc2", Name: "slow", Type: "webdav", Options: map[string]string{"url": srv.URL}})
	require.ErrorContains(t, err, "slow")
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestReachableListsMountPath(t *testing.T) {
	e := startEngine(t)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	s := store.Service{ID: "svc1", Name: "disk", Type: "alias", Options: map[string]string{"remote": dir}}
	require.NoError(t, e.PutService(s))
	require.NoError(t, e.Reachable(s, "sub"))
	require.Error(t, e.Reachable(s, "missing"))
}

func TestProbeListsCheaply(t *testing.T) {
	e := startEngine(t)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644))
	require.NoError(t, e.PutService(store.Service{ID: "probe", Type: "alias", Options: map[string]string{"remote": dir}}))

	in := probeParams("probe", "")
	require.Equal(t, map[string]any{"noModTime": true, "noMimeType": true, "dirsOnly": true}, in["opt"])
	var out struct {
		List []struct {
			Name, MimeType, ModTime string
		}
	}
	require.NoError(t, rpc("operations/list", in, &out))
	require.Len(t, out.List, 1)
	require.Equal(t, "sub", out.List[0].Name)
	require.Empty(t, out.List[0].MimeType)
	require.Empty(t, out.List[0].ModTime)
}

func TestMountAfterStopErrors(t *testing.T) {
	e, err := Start(t.TempDir(), nil)
	require.NoError(t, err)
	e.Stop()
	e.Stop()
	_, err = e.Mount(store.Mount{Name: "nas", ServiceID: "no-such-service", VFS: store.DefaultVFS()}, t.TempDir())
	require.ErrorContains(t, err, "stopped")
}

func TestStatsSkipsMountsRcloneDropped(t *testing.T) {
	e := startEngine(t)
	e.mounted["nas"] = liveMount{fs: "svc1:", point: filepath.Join(t.TempDir(), "nas"), serviceID: "svc1"}
	stats, err := e.Stats()
	require.NoError(t, err)
	require.Empty(t, stats)
	require.Contains(t, e.mounted, "nas")
}

func TestRemovePartialsDeletesOnlyLeftovers(t *testing.T) {
	e := startEngine(t)
	dir := t.TempDir()
	share := filepath.Join(dir, "share")
	require.NoError(t, e.PutService(store.Service{ID: "parts", Type: "alias", Options: map[string]string{"remote": dir}}))
	long := "sub/" + strings.Repeat("l", 120) + ".mov"
	const suffix = ".0123abcd.partial"
	leftovers := []string{
		"a.txt" + suffix,
		"sub/b.bin.deadbeef.partial",
		operations.TruncateString(long, len(long)-len(suffix)) + suffix,
	}
	kept := []string{
		"a.txt",
		"a.txt.partial",
		"a.txt.0123abcz.partial",
		"a.txt.0123abcd.partial.bak",
		"a.txt.0123abc.partial",
		"other.txt" + suffix,
		"sub/b.bin",
	}
	for _, f := range append(slices.Clone(leftovers), kept...) {
		p := filepath.Join(share, filepath.FromSlash(f))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
	}
	outside := filepath.Join(dir, "a.txt"+suffix)
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o644))

	m := store.Mount{ID: "m1", Name: "share", ServiceID: "parts", RemotePath: "share"}
	require.NoError(t, e.RemovePartials(t.Context(), m, []string{"a.txt", "sub/b.bin", long, "c.txt", "gone/x.txt"}), "a folder the upload never created holds no leftover")

	for _, f := range leftovers {
		require.NoFileExists(t, filepath.Join(share, filepath.FromSlash(f)))
	}
	for _, f := range kept {
		require.FileExists(t, filepath.Join(share, filepath.FromSlash(f)))
	}
	require.FileExists(t, outside)
}

func TestStopJobsStopsRunningJobs(t *testing.T) {
	e := startEngine(t)
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(hang) })
	require.NoError(t, e.PutService(store.Service{ID: "slow", Type: "webdav", Options: map[string]string{"url": srv.URL}}))
	var job struct{ JobID int64 }
	require.NoError(t, rpc("operations/list", map[string]any{"fs": "slow:", "remote": "", "_async": true}, &job))

	require.NoError(t, e.StopJobs())

	require.Eventually(t, func() bool {
		var st struct{ Finished bool }
		return rpc("job/status", map[string]any{"jobid": job.JobID}, &st) == nil && st.Finished
	}, 5*time.Second, 20*time.Millisecond)
}
