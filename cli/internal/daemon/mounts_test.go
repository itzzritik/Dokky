package daemon

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	pollEvery, goneTimeout, remountSettle, retryBase = time.Millisecond, 50*time.Millisecond, 0, 0
	os.Exit(m.Run())
}

type fakeEngine struct {
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
	if _, ok := f.mounted[m.Name]; ok {
		return "", fmt.Errorf("%s is already mounted", m.Name)
	}
	f.mounted[m.Name] = point
	f.os[point] = true
	f.mountedAt = time.Now()
	return point, nil
}

func (f *fakeEngine) Unmount(name string) error {
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
	var out []engine.MountStats
	for name := range f.mounted {
		out = append(out, engine.MountStats{Name: name})
	}
	return out, nil
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

func newTestDaemon(t *testing.T, data store.Data) (*Daemon, *fakeEngine, *store.Store) {
	t.Helper()
	t.Setenv("DOKKY_KEYRING", "file")
	dir := t.TempDir()
	st, err := store.Open(dir, store.SystemKeyring(dir))
	require.NoError(t, err)
	require.NoError(t, st.Save(data))
	f := newFakeEngine()
	d := newDaemon(f, st, data, t.TempDir(), dir, slog.New(slog.DiscardHandler))
	d.isMounted, d.forceUnmount = f.isMounted, f.forceUnmount
	return d, f, st
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
	require.NoError(t, d.mount("media"))

	state, _ := stateOf(d, "m1")
	require.Equal(t, stateMounted, state)
	require.True(t, load(t, st).Local["m1"].Mounted)
	require.Equal(t, d.defaultPoint("media"), f.mounted["media"])
	require.DirExists(t, d.defaultPoint("media"))

	require.Error(t, d.mount("nope"))
	require.Error(t, d.mount("Bad Name"))
}

func TestStartupRemountsOnlyFlagged(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample("m1"))
	d.start()

	require.Contains(t, f.mounted, "media")
	require.NotContains(t, f.mounted, "docs")
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
	d.tick()
	state, _ = stateOf(d, "m1")
	require.Equal(t, stateMounted, state)
}

func TestUnmountCleansMountPoint(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("media"))
	require.NoError(t, d.unmount("media"))

	require.NotContains(t, f.mounted, "media")
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

	require.ErrorContains(t, d.mount("media"), "already in use")
	require.NoError(t, d.unmount("media"))
	require.NoError(t, d.deleteMount("m1"))
	require.Empty(t, f.forced)
	require.DirExists(t, other)
}

func TestSetLocalRefusesWhileMounted(t *testing.T) {
	d, _, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("media"))
	require.ErrorContains(t, d.setLocal("m1", "/elsewhere"), "unmount")
	require.NoError(t, d.unmount("media"))
	require.NoError(t, d.setLocal("m1", "/elsewhere"))
	require.Equal(t, "/elsewhere", load(t, st).Local["m1"].MountPoint)
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
	go func() { mounted <- d.mount("media") }()
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
		d.tick()
		close(ticked)
	}()
	<-f.entered

	within(t, time.Second, func() { require.NoError(t, d.mount("docs")) })
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
	d.tick()
	require.Equal(t, probes, f.probes)

	f.failProbe = nil
	require.NoError(t, d.mount("media"))
}

func TestMountClearsStaleEngineEntry(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample())
	f.mounted["media"] = "/gone"
	f.failUnmount = 1
	require.NoError(t, d.mount("media"))
	require.Equal(t, d.defaultPoint("media"), f.mounted["media"])
}

func TestUnmountForcesWhenPathStays(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("media"))
	f.leaveOS = true
	require.NoError(t, d.unmount("media"))
	require.Equal(t, []string{d.defaultPoint("media")}, f.forced)
	require.False(t, load(t, st).Local["m1"].Mounted)
}

func TestUnmountFailsWhileStillMounted(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("media"))
	f.leaveOS, f.stuck = true, true
	require.Error(t, d.unmount("media"))
	state, _ := stateOf(d, "m1")
	require.Equal(t, stateError, state)
	require.True(t, load(t, st).Local["m1"].Mounted)
}

func TestRemountRightAfterUnmountWaits(t *testing.T) {
	remountSettle = 100 * time.Millisecond
	t.Cleanup(func() { remountSettle = 0 })
	d, f, _ := newTestDaemon(t, sample())
	require.NoError(t, d.mount("media"))
	require.NoError(t, d.unmount("media"))
	unmounted := time.Now()
	require.NoError(t, d.mount("media"))
	require.GreaterOrEqual(t, f.mountedAt.Sub(unmounted), 90*time.Millisecond)
}

func TestTickReapsEjectedMount(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("media"))
	require.NoError(t, f.Unmount("media"))
	d.tick()
	state, _ := stateOf(d, "m1")
	require.Equal(t, stateUnmounted, state)
	require.False(t, load(t, st).Local["m1"].Mounted)
	require.NoError(t, d.mount("media"))
}

func TestServiceDeleteUnmountsItsMounts(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("media"))
	require.NoError(t, d.deleteService("s1"))

	require.Empty(t, f.mounted)
	require.NotContains(t, f.services, "s1")
	got := load(t, st)
	require.Empty(t, got.Services)
	require.Empty(t, got.Mounts)
	require.Empty(t, got.Local)
}

func TestMountPutValidates(t *testing.T) {
	d, _, st := newTestDaemon(t, sample())
	require.Error(t, d.putMount(store.Mount{ID: "m3", Name: "Bad Name", ServiceID: "s1"}))
	require.Error(t, d.putMount(store.Mount{ID: "m3", Name: "media", ServiceID: "s1"}))
	require.Error(t, d.putMount(store.Mount{ID: "m3", Name: "new", ServiceID: "nope"}))
	require.NoError(t, d.putMount(store.Mount{ID: "m3", Name: "new", ServiceID: "s1", VFS: store.DefaultVFS()}))
	require.Len(t, load(t, st).Mounts, 3)
}
