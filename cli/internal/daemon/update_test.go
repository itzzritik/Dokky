package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
	"github.com/stretchr/testify/require"
)

var platformKey = runtime.GOOS + "-" + runtime.GOARCH

func runningAs(t *testing.T, v string) {
	t.Helper()
	old := buildinfo.Version
	buildinfo.Version = v
	t.Cleanup(func() { buildinfo.Version = old })
}

// a fake install: the "binary" is the text "dokky <version>"
func installed(t *testing.T, v string) Paths {
	t.Helper()
	root := t.TempDir()
	p := Paths{State: filepath.Join(root, "state"), Cache: filepath.Join(root, "cache"), Bin: filepath.Join(root, "bin", "dokky")}
	require.NoError(t, os.MkdirAll(p.State, 0o700))
	require.NoError(t, os.MkdirAll(filepath.Dir(p.Bin), 0o755))
	require.NoError(t, os.WriteFile(p.Bin, []byte("dokky "+v), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(p.State, "version"), []byte(v), 0o644))
	return p
}

type fakeRegistry struct{ hits atomic.Int32 }

// serves every version; tamper serves a binary that doesn't match its hash
func newRegistry(t *testing.T, latest string, tamper bool) *fakeRegistry {
	t.Helper()
	r := &fakeRegistry{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dokky/{v}", func(w http.ResponseWriter, req *http.Request) {
		r.hits.Add(1)
		v := req.PathValue("v")
		if v == "latest" {
			v = latest
		}
		sum := sha256.Sum256([]byte("dokky " + v))
		writeJSON(w, http.StatusOK, map[string]any{"version": v, "dokky": map[string]any{"binaries": map[string]any{
			platformKey: map[string]string{"url": "http://" + req.Host + "/releases/download/v" + v + "/dokky-" + platformKey, "sha256": hex.EncodeToString(sum[:])},
		}}})
	})
	mux.HandleFunc("GET /releases/download/{tag}/{file}", func(w http.ResponseWriter, req *http.Request) {
		body := "dokky " + strings.TrimPrefix(req.PathValue("tag"), "v")
		if tamper {
			body = "evil"
		}
		_, _ = io.WriteString(w, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	oldRegistry, oldBase := registry, releaseBase
	registry, releaseBase = srv.URL, srv.URL+"/releases/download/"
	t.Cleanup(func() { registry, releaseBase = oldRegistry, oldBase })
	return r
}

func fakeSelftest(t *testing.T, fail error) *[]string {
	t.Helper()
	var ran []string
	old := runSelftest
	runSelftest = func(_ context.Context, bin, _ string) error {
		ran = append(ran, bin)
		return fail
	}
	t.Cleanup(func() { runSelftest = old })
	return &ran
}

func latestRelease(t *testing.T) Release {
	t.Helper()
	r, err := Latest(t.Context(), http.DefaultClient, "latest", platformKey)
	require.NoError(t, err)
	return r
}

func requireInstalled(t *testing.T, p Paths, bin, version string) {
	t.Helper()
	got, err := os.ReadFile(p.Bin)
	require.NoError(t, err)
	require.Equal(t, "dokky "+bin, string(got))
	v, err := os.ReadFile(filepath.Join(p.State, "version"))
	require.NoError(t, err)
	require.Equal(t, version, string(v))
}

func requireOldBinaryKept(t *testing.T, p Paths) {
	t.Helper()
	requireInstalled(t, p, "0.1.0", "0.1.0")
	require.NoFileExists(t, filepath.Join(p.State, "dokky.old"))
	entries, err := os.ReadDir(filepath.Join(p.State, "updates"))
	if !errors.Is(err, os.ErrNotExist) {
		require.NoError(t, err)
	}
	require.Empty(t, entries, "the download must be removed")
}

func TestInstallHashMismatchKeepsOldBinary(t *testing.T) {
	newRegistry(t, "0.2.0", true)
	ran := fakeSelftest(t, nil)
	p := installed(t, "0.1.0")
	require.ErrorContains(t, Install(t.Context(), latestRelease(t), p), "sha256")
	require.Empty(t, *ran, "an unverified binary must never run")
	requireOldBinaryKept(t, p)
}

func TestInstallSelftestFailureKeepsOldBinary(t *testing.T) {
	newRegistry(t, "0.2.0", false)
	fakeSelftest(t, errors.New("engine did not start"))
	p := installed(t, "0.1.0")
	require.ErrorContains(t, Install(t.Context(), latestRelease(t), p), "engine did not start")
	requireOldBinaryKept(t, p)
}

func TestInstallKeepsOldBinaryAndWritesVersion(t *testing.T) {
	newRegistry(t, "0.2.0", false)
	ran := fakeSelftest(t, nil)
	p := installed(t, "0.1.0")
	require.NoError(t, os.WriteFile(filepath.Join(p.State, "dokky.old"), []byte("dokky 0.0.1"), 0o755))

	require.NoError(t, Install(t.Context(), latestRelease(t), p))
	require.Equal(t, []string{filepath.Join(p.State, "updates", "dokky-0.2.0")}, *ran)
	requireInstalled(t, p, "0.2.0", "0.2.0")
	old, err := os.ReadFile(filepath.Join(p.State, "dokky.old"))
	require.NoError(t, err)
	require.Equal(t, "dokky 0.1.0", string(old))
	entries, err := os.ReadDir(filepath.Join(p.State, "updates"))
	require.NoError(t, err)
	require.Empty(t, entries)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(p.Bin)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o755), fi.Mode().Perm())
	}
}

func TestInstallRollsBackWhenMoveFails(t *testing.T) {
	newRegistry(t, "0.2.0", false)
	fakeSelftest(t, nil)
	p := installed(t, "0.1.0")
	old := rename
	rename = func(from, to string) error {
		if to == p.Bin && filepath.Dir(from) != p.State {
			return errors.New("disk full")
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { rename = old })

	require.ErrorContains(t, Install(t.Context(), latestRelease(t), p), "disk full")
	requireOldBinaryKept(t, p)
}

func TestLatestRejectsBadReplies(t *testing.T) {
	reply := func(version string, binaries map[string]any) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"version": version, "dokky": map[string]any{"binaries": binaries}})
		}
	}
	file := "https://github.com/itzzritik/dokky/releases/download/v0.2.0/dokky-" + platformKey
	hash := strings.Repeat("ab", 32)
	entry := func(url, sha string) map[string]any {
		return map[string]any{platformKey: map[string]string{"url": url, "sha256": sha}}
	}
	lookup := func(t *testing.T, h http.HandlerFunc) error {
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		old := registry
		registry = srv.URL
		t.Cleanup(func() { registry = old })
		_, err := Latest(t.Context(), srv.Client(), "0.2.0", platformKey)
		return err
	}
	require.NoError(t, lookup(t, reply("0.2.0", entry(file, hash))))
	for name, h := range map[string]http.HandlerFunc{
		"not found":     http.NotFound,
		"server error":  func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"malformed":     func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "{") },
		"no binary":     reply("0.2.0", map[string]any{"plan9-amd64": entry(file, hash)[platformKey]}),
		"other version": reply("0.3.0", entry(file, hash)),
		"http url":      reply("0.2.0", entry(strings.Replace(file, "https://", "http://", 1), hash)),
		"other host":    reply("0.2.0", entry(strings.Replace(file, "github.com", "evil.example", 1), hash)),
		"other tag":     reply("0.2.0", entry(strings.Replace(file, "/v0.2.0/", "/v0.1.0/", 1), hash)),
		"short hash":    reply("0.2.0", entry(file, "ab")),
		"non-hex hash":  reply("0.2.0", entry(file, strings.Repeat("zz", 32))),
		"too large": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(bytes.Repeat([]byte(" "), maxMeta+1))
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, lookup(t, h))
		})
	}
	reg := newRegistry(t, "0.2.0", false)
	_, err := Latest(t.Context(), http.DefaultClient, "../evil", platformKey)
	require.Error(t, err)
	require.Zero(t, reg.hits.Load())
}

func updateDaemon(t *testing.T, running string) (*Daemon, *store.Store) {
	t.Helper()
	t.Setenv("DOKKY_NO_UPDATE", "")
	runningAs(t, running)
	fakeSelftest(t, nil)
	data := sample()
	data.Settings.AutoUpdate = true
	d, _, st := newTestDaemon(t, data)
	d.upd.paths = installed(t, running)
	return d, st
}

func TestUpdateToPinsAndPlainUpdateUnpins(t *testing.T) {
	d, st := updateDaemon(t, "0.1.0")
	p := d.upd.paths
	reg := newRegistry(t, "0.2.0", false)

	var r updateReply
	require.NoError(t, call(t, d, "update", updateParams{To: "0.0.9"}, &r))
	require.Equal(t, updateReply{Version: "0.0.9", Updated: true}, r)
	requireInstalled(t, p, "0.0.9", "0.0.9")
	require.Equal(t, store.Settings{AutoUpdate: false, PinnedVersion: "0.0.9"}, d.settings())
	require.Equal(t, d.settings(), load(t, st).Settings)
	pin, err := os.ReadFile(filepath.Join(p.State, "pin"))
	require.NoError(t, err)
	require.Equal(t, "0.0.9", string(pin))

	hits := reg.hits.Load()
	d.upd.ticks = 0
	require.False(t, d.autoUpdate(t.Context(), t0))
	require.Equal(t, hits, reg.hits.Load(), "a pinned version turns auto update off")

	require.NoError(t, call(t, d, "update", updateParams{}, &r))
	require.Equal(t, updateReply{Version: "0.2.0", Updated: true}, r)
	requireInstalled(t, p, "0.2.0", "0.2.0")
	require.Equal(t, store.Settings{AutoUpdate: true}, d.settings())
	require.Equal(t, d.settings(), load(t, st).Settings)
	require.NoFileExists(t, filepath.Join(p.State, "pin"))

	require.NoError(t, call(t, d, "update", updateParams{}, &r))
	require.Equal(t, updateReply{Version: "0.2.0", Updated: false}, r)
}

func TestPlainUpdateKeepsAutoUpdateOff(t *testing.T) {
	d, st := updateDaemon(t, "0.1.0")
	require.NoError(t, d.setSettings(store.Settings{}))
	newRegistry(t, "0.2.0", false)
	require.NoError(t, call(t, d, "update", updateParams{}, nil))
	requireInstalled(t, d.upd.paths, "0.2.0", "0.2.0")
	require.Equal(t, store.Settings{}, d.settings())
	require.Equal(t, store.Settings{}, load(t, st).Settings)
}

func TestWipeClearsPin(t *testing.T) {
	d, _ := updateDaemon(t, "0.1.0")
	require.NoError(t, d.setSettings(store.Settings{PinnedVersion: "0.0.9"}))
	require.FileExists(t, filepath.Join(d.upd.paths.State, "pin"))
	d.opMu.Lock()
	require.NoError(t, d.forget())
	d.opMu.Unlock()
	require.NoFileExists(t, filepath.Join(d.upd.paths.State, "pin"))
	require.Equal(t, store.Fresh().Settings, d.settings())
}

func TestFailedUpdateChangesNothing(t *testing.T) {
	d, _ := updateDaemon(t, "0.1.0")
	newRegistry(t, "0.2.0", true)
	require.Error(t, call(t, d, "update", updateParams{To: "0.2.0"}, nil))
	requireOldBinaryKept(t, d.upd.paths)
	require.Equal(t, store.Settings{AutoUpdate: true}, d.settings())
}

func TestAutoUpdateNeverInstallsOlderOrSame(t *testing.T) {
	for _, latest := range []string{"0.0.9", "0.1.0", "0.1.0-rc.1"} {
		t.Run(latest, func(t *testing.T) {
			d, _ := updateDaemon(t, "0.1.0")
			reg := newRegistry(t, latest, false)
			d.idle.Observe(t0, 0, 0)
			require.False(t, d.autoUpdate(t.Context(), t0.Add(idleAfter)))
			require.EqualValues(t, 1, reg.hits.Load())
			requireOldBinaryKept(t, d.upd.paths)
		})
	}
}

func TestAutoUpdateOff(t *testing.T) {
	for name, off := range map[string]func(*testing.T, *Daemon){
		"env":     func(t *testing.T, _ *Daemon) { t.Setenv("DOKKY_NO_UPDATE", "1") },
		"setting": func(t *testing.T, d *Daemon) { require.NoError(t, d.setSettings(store.Settings{})) },
	} {
		t.Run(name, func(t *testing.T) {
			d, _ := updateDaemon(t, "0.1.0")
			reg := newRegistry(t, "0.2.0", false)
			off(t, d)
			d.note(fmt.Errorf("%w: x", dsync.ErrUpgrade))
			require.False(t, d.autoUpdate(t.Context(), t0))
			require.Zero(t, reg.hits.Load())
			requireOldBinaryKept(t, d.upd.paths)
		})
	}
}

func TestUpdateBlockedForDevOrForeignBinary(t *testing.T) {
	p := installed(t, "0.1.0")
	exe := p.Bin
	old := executable
	executable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executable = old })

	runningAs(t, "0.1.0")
	require.Empty(t, updateBlocked(p.Bin))
	if runtime.GOOS != "windows" {
		exe = filepath.Join(t.TempDir(), "dokky")
		require.NoError(t, os.Symlink(p.Bin, exe))
		require.Empty(t, updateBlocked(p.Bin), "a symlink to the binary is the binary")
	}
	exe = filepath.Join(t.TempDir(), "dokky")
	require.NoError(t, os.WriteFile(exe, []byte("dokky 0.1.0"), 0o755))
	require.Contains(t, updateBlocked(p.Bin), exe)
	exe = p.Bin
	runningAs(t, "0.0.0-dev")
	require.Contains(t, updateBlocked(p.Bin), "dev")

	d, _ := updateDaemon(t, "0.1.0")
	d.upd.blocked = "dev build"
	reg := newRegistry(t, "0.2.0", false)
	require.NoError(t, os.WriteFile(filepath.Join(d.upd.paths.State, "version"), []byte("0.2.0"), 0o644))
	d.idle.Observe(t0, 0, 0)
	require.False(t, d.autoUpdate(t.Context(), t0.Add(idleAfter)))
	require.Zero(t, reg.hits.Load())
}

func TestAutoUpdateRestartsOnlyWhenIdle(t *testing.T) {
	d, _ := updateDaemon(t, "0.1.0")
	reg := newRegistry(t, "0.2.0", false)
	d.idle.Observe(t0, 1, 0)
	require.False(t, d.autoUpdate(t.Context(), t0))
	requireInstalled(t, d.upd.paths, "0.2.0", "0.2.0")
	require.FileExists(t, filepath.Join(d.upd.paths.State, "dokky.old"))

	later := t0.Add(time.Hour)
	require.False(t, d.autoUpdate(t.Context(), later), "uploads are running")
	d.idle.Observe(later, 0, 0)
	require.False(t, d.autoUpdate(t.Context(), later))
	require.True(t, d.autoUpdate(t.Context(), later.Add(idleAfter)))
	require.EqualValues(t, 1, reg.hits.Load(), "no new check while a restart is due")
}

func TestNoRestartOnTheCallThatInstalls(t *testing.T) {
	d, _ := updateDaemon(t, "0.1.0")
	newRegistry(t, "0.2.0", false)
	d.idle.Observe(t0, 0, 0)
	idle := t0.Add(idleAfter)
	require.False(t, d.autoUpdate(t.Context(), idle), "idle was observed before the download, not after")
	requireInstalled(t, d.upd.paths, "0.2.0", "0.2.0")
	busy := idle.Add(time.Minute)
	d.idle.Observe(busy, 1, 0)
	require.False(t, d.autoUpdate(t.Context(), busy), "a copy started during the download")
}

func TestStartFixesAWrongVersionFile(t *testing.T) {
	p := installed(t, "0.1.0")
	require.NoError(t, os.WriteFile(filepath.Join(p.State, "version"), []byte("0.2.0"), 0o644))
	old := executable
	executable = func() (string, error) { return p.Bin, nil }
	t.Cleanup(func() { executable = old })
	d, _, _ := newTestDaemon(t, sample())

	runningAs(t, "0.0.0-dev")
	d.startUpdater(p)
	require.NotEmpty(t, d.upd.blocked)
	requireInstalled(t, p, "0.1.0", "0.2.0")

	runningAs(t, "0.1.0")
	d.startUpdater(p)
	require.Empty(t, d.upd.blocked)
	requireInstalled(t, p, "0.1.0", "0.1.0")
}

func TestNewerVersionFileRestartsWhenIdle(t *testing.T) {
	d, _ := updateDaemon(t, "0.1.0")
	t.Setenv("DOKKY_NO_UPDATE", "1")
	d.idle.Observe(t0, 0, 0)
	idle := t0.Add(idleAfter)
	require.False(t, d.autoUpdate(t.Context(), idle))

	require.NoError(t, os.WriteFile(filepath.Join(d.upd.paths.State, "version"), []byte("0.2.0"), 0o644))
	require.False(t, d.autoUpdate(t.Context(), t0))
	require.True(t, d.autoUpdate(t.Context(), idle))
}

func TestChecksOnStartEverySixHoursAndOnUpgrade(t *testing.T) {
	d, _ := updateDaemon(t, "0.1.0")
	reg := newRegistry(t, "0.1.0", false)
	for range checkTicks {
		d.autoUpdate(t.Context(), t0)
	}
	require.EqualValues(t, 1, reg.hits.Load())
	d.autoUpdate(t.Context(), t0)
	require.EqualValues(t, 2, reg.hits.Load())

	d.note(fmt.Errorf("%w: needs 0.2.0", dsync.ErrUpgrade))
	d.autoUpdate(t.Context(), t0)
	require.EqualValues(t, 3, reg.hits.Load())
}
