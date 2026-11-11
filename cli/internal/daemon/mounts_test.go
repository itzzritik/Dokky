package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/fs"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	fs.GetConfig(context.Background()).LogLevel = fs.LogLevelCritical
	pollEvery, goneTimeout, remountSettle, retryBase, uploadWait = time.Millisecond, 50*time.Millisecond, 0, 0, time.Millisecond
	removeFinderHost = func() error { return nil }
	os.Exit(m.Run())
}

type fakeEngine struct {
	notes       []string
	cleaned     []string
	mountEngine // methods the fake leaves out panic if called
	mu          sync.Mutex
	mounted     map[string]string
	os          map[string]bool
	failMount   error
	failProbe   error
	failUnmount int
	leaveOS     bool
	stuck       bool
	forced      []string
	mountedAt   time.Time
	probes      int
	services    map[string]store.Service
	uploads     int
	failStats   error
	totalBytes  int64
	failTotal   error
	tested      store.Service
	failTest    error
	onUnmount   func(name string)

	// rclone holds its mount lock across a whole mount, and Stats needs it
	rclone               sync.Mutex
	hangProbe, hangMount string
	entered, gate        chan struct{}
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		mounted: map[string]string{}, os: map[string]bool{}, services: map[string]store.Service{},
		entered: make(chan struct{}, 1), gate: make(chan struct{}),
	}
}

func (f *fakeEngine) hang() {
	select {
	case f.entered <- struct{}{}:
	default:
	}
	<-f.gate
}

func (f *fakeEngine) Reachable(s store.Service, _ string) error {
	if s.ID == f.hangProbe {
		f.hang()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes++
	return f.failProbe
}

func (f *fakeEngine) Mount(m store.Mount, point string) (string, error) {
	f.rclone.Lock()
	defer f.rclone.Unlock()
	if m.ServiceID == f.hangMount {
		f.hang()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failMount != nil {
		return "", f.failMount
	}
	if _, ok := f.mounted[m.ID]; ok {
		return "", fmt.Errorf("%s is already mounted", m.Name)
	}
	f.mounted[m.ID] = point
	f.os[point] = true
	f.mountedAt = time.Now()
	return point, nil
}

func (f *fakeEngine) Unmount(name string) error {
	if f.onUnmount != nil {
		f.onUnmount(name)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failUnmount > 0 {
		f.failUnmount--
		return errors.New("rclone unmount failed")
	}
	if p, ok := f.mounted[name]; ok && !f.leaveOS {
		delete(f.os, p)
	}
	delete(f.mounted, name)
	return nil
}

func (f *fakeEngine) PutService(s store.Service) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.services[s.ID] = s
	return nil
}

func (f *fakeEngine) TestRemote(s store.Service) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tested = s
	return f.failTest
}

func (f *fakeEngine) DeleteService(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.services, id)
}

func (f *fakeEngine) Stats() ([]engine.MountStats, error) {
	f.rclone.Lock()
	defer f.rclone.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failStats != nil {
		return nil, f.failStats
	}
	var out []engine.MountStats
	for name := range f.mounted {
		out = append(out, engine.MountStats{ID: name, UploadsQueued: f.uploads})
	}
	return out, nil
}

func (f *fakeEngine) CleanupUploads(serviceID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleaned = append(f.cleaned, serviceID)
	return nil
}

func (f *fakeEngine) cleanups() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.cleaned)
}

func (f *fakeEngine) TotalBytes() (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.totalBytes, f.failTotal
}

func (f *fakeEngine) isMounted(point string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.os[point]
}

func (f *fakeEngine) forceUnmount(point string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forced = append(f.forced, point)
	if f.stuck {
		return errors.New("busy")
	}
	delete(f.os, point)
	return nil
}

func sample(flagged ...string) store.Data {
	d := store.Data{
		Services: []store.Service{{ID: "s1", Name: "nas", Type: "webdav", Options: map[string]string{"url": "http://nas"}, Secrets: map[string]string{"pass": "hunter2-secret"}}},
		Mounts: []store.Mount{
			{ID: "m1", Name: "media", ServiceID: "s1", VFS: store.DefaultVFS()},
			{ID: "m2", Name: "docs", ServiceID: "s1", VFS: store.DefaultVFS()},
		},
		Local: map[string]store.LocalMount{},
	}
	for _, id := range flagged {
		d.Local[id] = store.LocalMount{Mounted: true}
	}
	return d
}

// newTestDaemon is unlocked with the shared test vault, linked to the account when data has a finished one
func newTestDaemon(t *testing.T, data store.Data) (*Daemon, *fakeEngine, *store.Store) {
	t.Helper()
	t.Setenv("DOKKY_KEYRING", "file")
	dir := t.TempDir()
	k := unlockedKeeper(t, dir, data.Account != nil && !data.Account.Pending)
	st, err := store.OpenWithKey(dir, testVault().ak)
	require.NoError(t, err)
	require.NoError(t, st.Save(data))
	return testDaemon(t, dir, k, st, data)
}

// newSetupDaemon has no master password yet: the store uses the old keychain key, as before vaults
func newSetupDaemon(t *testing.T, data store.Data) (*Daemon, *fakeEngine, *store.Store) {
	t.Helper()
	t.Setenv("DOKKY_KEYRING", "file")
	dir := t.TempDir()
	st, err := store.Open(dir, store.SystemKeyring(dir))
	require.NoError(t, err)
	require.NoError(t, st.Save(data))
	return testDaemon(t, dir, testKeeper(dir), st, data)
}

func testDaemon(t *testing.T, dir string, k *keeper, st *store.Store, data store.Data) (*Daemon, *fakeEngine, *store.Store) {
	t.Helper()
	f := newFakeEngine()
	d := newDaemon(f, st, data, t.TempDir(), dir, slog.New(slog.DiscardHandler), k)
	d.isMounted, d.forceUnmount = f.isMounted, f.forceUnmount
	d.notify = func(_, body string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.notes = append(f.notes, body)
		return nil
	}
	d.upd.paths.State = dir
	inSession(k)
	return d, f, st
}

func (f *fakeEngine) notices() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.notes)
}

func load(t *testing.T, st *store.Store) store.Data {
	t.Helper()
	d, err := st.Load()
	require.NoError(t, err)
	return d
}

func stateOf(d *Daemon, id string) (string, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if st := d.states[id]; st != nil {
		return st.state, st.err
	}
	return stateUnmounted, ""
}

func TestMountMarksMountedAndSaves(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("m1"))

	state, _ := stateOf(d, "m1")
	require.Equal(t, stateMounted, state)
	require.True(t, load(t, st).Local["m1"].Mounted)
	require.Equal(t, d.defaultPoint("media"), f.mounted["m1"])
	if runtime.GOOS != "windows" {
		require.DirExists(t, d.defaultPoint("media"))
	}

	require.Error(t, d.mount("nope"))
	require.Error(t, d.mount("Bad Name"))
}

func TestStartupRemountsOnlyFlagged(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample("m1"))
	d.start()

	require.Contains(t, f.mounted, "m1")
	require.NotContains(t, f.mounted, "m2")
	require.Contains(t, f.services, "s1")
	state, _ := stateOf(d, "m2")
	require.Equal(t, stateUnmounted, state)
}

func TestStartupOfflineKeepsFlag(t *testing.T) {
	d, f, st := newTestDaemon(t, sample("m1"))
	f.failMount = errors.New("dial tcp: no route to host")
	d.start()

	state, msg := stateOf(d, "m1")
	require.Equal(t, stateError, state)
	require.Contains(t, msg, "no route to host")
	require.True(t, load(t, st).Local["m1"].Mounted)

	f.failMount = nil
	d.tick(context.Background())
	state, _ = stateOf(d, "m1")
	require.Equal(t, stateMounted, state)
}

func TestUnmountCleansMountPoint(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("m1"))
	require.NoError(t, d.unmount("m1"))

	require.NotContains(t, f.mounted, "m1")
	require.NoDirExists(t, d.defaultPoint("media"))
	require.NoDirExists(t, filepath.Join(d.home, "dokky"))
	require.False(t, load(t, st).Local["m1"].Mounted)
	state, _ := stateOf(d, "m1")
	require.Equal(t, stateUnmounted, state)
}

func TestStartupForcesStaleMountOff(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample("m1"))
	f.os[d.defaultPoint("media")] = true
	d.start()

	require.Equal(t, []string{d.defaultPoint("media")}, f.forced)
	state, _ := stateOf(d, "m1")
	require.Equal(t, stateMounted, state)
}

func TestForeignMountIsNeverForced(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample())
	other := filepath.Join(t.TempDir(), "volume")
	require.NoError(t, os.Mkdir(other, 0o755))
	f.os[other] = true
	require.NoError(t, d.setLocal("m1", other))

	require.ErrorContains(t, d.mount("m1"), "already in use")
	require.NoError(t, d.unmount("m1"))
	require.NoError(t, d.deleteMount(context.Background(), "m1", false))
	require.Empty(t, f.forced)
	require.DirExists(t, other)
}

func TestSetLocalRefusesWhileMounted(t *testing.T) {
	d, _, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("m1"))
	other := filepath.Join(t.TempDir(), "elsewhere")
	require.ErrorContains(t, d.setLocal("m1", other), "unmount")
	require.NoError(t, d.unmount("m1"))
	require.NoError(t, d.setLocal("m1", other))
	require.Equal(t, other, load(t, st).Local["m1"].MountPoint)
	require.Error(t, d.setLocal("m1", "relative"))
}

func within(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("did not return within %s", d)
	}
}

func TestStatusAnswersWhileMountIsStuck(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample())
	f.hangMount = "s1"
	mounted := make(chan error, 1)
	go func() { mounted <- d.mount("m1") }()
	<-f.entered

	var r StatusReply
	within(t, time.Second, func() { r = d.status() })
	require.Equal(t, stateMounting, r.Mounts[0].State)

	close(f.gate)
	require.NoError(t, <-mounted)
}

func TestCommandsDontWaitOnOfflineRetry(t *testing.T) {
	data := sample("m1")
	data.Services = append(data.Services, store.Service{ID: "s2", Name: "other", Type: "webdav"})
	data.Mounts[1].ServiceID = "s2"
	d, f, _ := newTestDaemon(t, data)
	f.hangProbe, f.hangMount = "s1", "s1"
	ticked := make(chan struct{})
	go func() {
		d.tick(context.Background())
		close(ticked)
	}()
	<-f.entered

	within(t, time.Second, func() { require.NoError(t, d.mount("m2")) })
	within(t, time.Second, func() { d.status() })

	close(f.gate)
	<-ticked
}

func TestOfflineRetryBacksOff(t *testing.T) {
	retryBase = time.Hour
	t.Cleanup(func() { retryBase = 0 })
	d, f, st := newTestDaemon(t, sample("m1"))
	f.failProbe = errors.New("dial tcp: no route to host")
	d.start()
	state, msg := stateOf(d, "m1")
	require.Equal(t, stateError, state)
	require.Contains(t, msg, "no route to host")
	require.True(t, load(t, st).Local["m1"].Mounted)

	probes := f.probes
	d.tick(context.Background())
	require.Equal(t, probes, f.probes)

	f.failProbe = nil
	require.NoError(t, d.mount("m1"))
}

func TestMountClearsStaleEngineEntry(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample())
	f.mounted["m1"] = "/gone"
	f.failUnmount = 1
	require.NoError(t, d.mount("m1"))
	require.Equal(t, d.defaultPoint("media"), f.mounted["m1"])
}

func TestUnmountForcesWhenPathStays(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("m1"))
	f.leaveOS = true
	require.NoError(t, d.unmount("m1"))
	require.Equal(t, []string{d.defaultPoint("media")}, f.forced)
	require.False(t, load(t, st).Local["m1"].Mounted)
}

func TestUnmountFailsWhileStillMounted(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("m1"))
	f.leaveOS, f.stuck = true, true
	require.Error(t, d.unmount("m1"))
	state, _ := stateOf(d, "m1")
	require.Equal(t, stateError, state)
	require.True(t, load(t, st).Local["m1"].Mounted)
}

func TestRemountRightAfterUnmountWaits(t *testing.T) {
	remountSettle = 100 * time.Millisecond
	t.Cleanup(func() { remountSettle = 0 })
	d, f, _ := newTestDaemon(t, sample())
	require.NoError(t, d.mount("m1"))
	require.NoError(t, d.unmount("m1"))
	unmounted := time.Now()
	require.NoError(t, d.mount("m1"))
	require.GreaterOrEqual(t, f.mountedAt.Sub(unmounted), 90*time.Millisecond)
}

func TestTickReapsEjectedMount(t *testing.T) {
	data := sample()
	data.Settings.NotifyDisconnect = true
	d, f, st := newTestDaemon(t, data)
	require.NoError(t, d.mount("m1"))
	require.NoError(t, d.mount("m2"))
	require.NoError(t, f.Unmount("m1"))
	require.NoError(t, f.Unmount("m2"))
	d.tick(context.Background())
	for _, id := range []string{"m1", "m2"} {
		state, _ := stateOf(d, id)
		require.Equal(t, stateUnmounted, state)
		require.False(t, load(t, st).Local[id].Mounted)
	}
	require.Eventually(t, func() bool { return len(f.notices()) == 1 }, time.Second, 5*time.Millisecond)
	require.Contains(t, f.notices()[0], "docs, media")
	require.Contains(t, f.notices()[0], "Open dokky to mount")

	require.NoError(t, d.mount("m1"))
	require.NoError(t, d.unmount("m1"))
	d.tick(context.Background())
	time.Sleep(20 * time.Millisecond)
	require.Len(t, f.notices(), 1, "unmounting from dokky is not an eject")
}

func TestServiceDeleteUnmountsItsMounts(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("m1"))
	require.NoError(t, d.deleteService(context.Background(), "s1"))

	require.Empty(t, f.mounted)
	require.NotContains(t, f.services, "s1")
	got := load(t, st)
	require.Empty(t, got.Services)
	require.Empty(t, got.Mounts)
	require.Empty(t, got.Local)
}

func TestMountPutValidates(t *testing.T) {
	d, _, st := newTestDaemon(t, sample())
	require.Error(t, d.putMount(context.Background(), store.Mount{ID: "m3", Name: "a/b", ServiceID: "s1"}))
	require.ErrorContains(t, d.putMount(context.Background(), store.Mount{ID: "m3", Name: "Media", ServiceID: "s1", VFS: store.DefaultVFS()}), "already uses the folder")
	require.ErrorContains(t, d.putMount(context.Background(), store.Mount{ID: "m3", Name: "new", ServiceID: "nope", VFS: store.DefaultVFS()}), "no service")
	require.ErrorContains(t, d.putMount(context.Background(), store.Mount{ID: "m3", Name: "new", ServiceID: "s1"}), "cache max age")
	require.NoError(t, d.putMount(context.Background(), store.Mount{ID: "m3", Name: "new", ServiceID: "s1", VFS: store.DefaultVFS()}))
	require.Len(t, load(t, st).Mounts, 3)

	var reply struct{ ID string }
	require.NoError(t, call(t, d, "mount.put", store.Mount{Name: "fresh", ServiceID: "s1", VFS: store.DefaultVFS()}, &reply))
	require.NoError(t, uuid.Validate(reply.ID))
	require.Equal(t, reply.ID, load(t, st).Mounts[3].ID)
}

func TestMountPutCacheMaxAge(t *testing.T) {
	d, f, st := newTestDaemon(t, sample("m1"))
	d.start()
	require.Contains(t, f.mounted, "m1")

	var m store.Mount
	require.NoError(t, call(t, d, "mount.get", idParams{ID: "m1"}, &m))
	require.Equal(t, "media", m.Name)
	require.Equal(t, "5m", d.mountStatus("m1").CacheMaxAge)

	m.VFS.CacheMaxAge = "2h"
	require.ErrorContains(t, call(t, d, "mount.put", m, nil), "cache max age")
	require.Equal(t, "5m", load(t, st).Mounts[0].VFS.CacheMaxAge)

	m.VFS.CacheMaxAge = "1h"
	require.NoError(t, call(t, d, "mount.put", m, nil))
	require.Equal(t, "1h", load(t, st).Mounts[0].VFS.CacheMaxAge)
	require.Equal(t, "1h", d.mountStatus("m1").CacheMaxAge)
	require.True(t, d.mountStatus("m1").RemountToApply)

	docs := load(t, st).Mounts[1]
	docs.VFS.CacheMaxAge = "7d"
	require.NoError(t, call(t, d, "mount.put", docs, nil))
	require.False(t, d.mountStatus("m2").RemountToApply)

	require.NoError(t, d.unmount("m1"))
	require.NoError(t, d.mount("m1"))
	require.False(t, d.mountStatus("m1").RemountToApply)
	require.ErrorContains(t, call(t, d, "mount.get", idParams{ID: "nope"}, nil), "no mount")
}

func TestCleansS3UploadsDaily(t *testing.T) {
	data := sample()
	data.Services = append(data.Services, store.Service{ID: "s3", Name: "nas3", Type: "s3"})
	d, f, _ := newTestDaemon(t, data)
	t0 := time.Now()

	d.cleanupUploads(t0)
	require.Eventually(t, func() bool { return len(f.cleanups()) == 1 }, time.Second, 5*time.Millisecond)
	require.Equal(t, []string{"s3"}, f.cleanups(), "only S3 services")

	d.cleanupUploads(t0.Add(23 * time.Hour))
	time.Sleep(20 * time.Millisecond)
	require.Len(t, f.cleanups(), 1, "at most once a day")

	d.cleanupUploads(t0.Add(25 * time.Hour))
	require.Eventually(t, func() bool { return len(f.cleanups()) == 2 }, time.Second, 5*time.Millisecond)
}
