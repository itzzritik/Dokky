package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// the Windows update bug: drives dokky took down itself must come back on the next start
func TestShutdownKeepsFlagsThroughLateRefresh(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("m1"))
	require.NoError(t, d.mount("m2"))

	d.shutdown()
	require.Empty(t, f.mounted)
	// anything that refreshes after shutdown, before the process is gone, must not read the drives as ejected
	require.NoError(t, d.refreshStats())
	d.tick(context.Background())
	for _, id := range []string{"m1", "m2"} {
		require.True(t, load(t, st).Local[id].Mounted, id)
	}
	require.Empty(t, f.notices())

	next, f2, _ := newTestDaemonOn(t, st)
	next.start()
	require.Contains(t, f2.mounted, "m1")
	require.Contains(t, f2.mounted, "m2")
}

func TestDriveLostDuringInstallIsNotAnEject(t *testing.T) {
	d, f, st := newTestDaemon(t, sample())
	require.NoError(t, d.mount("m1"))
	d.upd.installing.Store(true)
	require.NoError(t, f.Unmount("m1"))
	require.NoError(t, d.refreshStats())
	d.upd.installing.Store(false)

	state, _ := stateOf(d, "m1")
	require.Equal(t, stateUnmounted, state)
	require.True(t, load(t, st).Local["m1"].Mounted, "kept, so the next tick or start mounts it again")
	require.Empty(t, f.notices())

	d.tick(context.Background())
	state, _ = stateOf(d, "m1")
	require.Equal(t, stateMounted, state)
}

func TestResumeNoteBringsBackDrives(t *testing.T) {
	d, _, st := newTestDaemon(t, sample())
	state := t.TempDir()
	require.NoError(t, d.mount("m1"))
	d.noteResume(state)
	require.FileExists(t, filepath.Join(state, resumeFile))

	// whatever cleared the flag between the note and the next start
	data := load(t, st)
	lm := data.Local["m1"]
	lm.Mounted = false
	data.Local["m1"] = lm
	require.NoError(t, st.Save(data))

	next, f2, _ := newTestDaemonOn(t, st)
	next.loadServices()
	next.remountAtStart(state)
	require.Contains(t, f2.mounted, "m1")
	require.NotContains(t, f2.mounted, "m2")
	require.True(t, load(t, st).Local["m1"].Mounted)
	require.NoFileExists(t, filepath.Join(state, resumeFile), "a note is used once")
}

func TestResumeNoteIgnoredWhenOldOrRemoving(t *testing.T) {
	cases := map[string]func(*resumeNote, *Daemon){
		"old": func(n *resumeNote, _ *Daemon) { n.At = time.Now().Add(-time.Hour) },
		"removing": func(_ *resumeNote, d *Daemon) {
			lm := d.data.Local["m1"]
			lm.Removing = true
			d.data.Local["m1"] = lm
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			d, _, st := newTestDaemon(t, sample())
			state := t.TempDir()
			n := resumeNote{At: time.Now(), Mounts: []string{"m1"}}
			change(&n, d)
			raw, err := json.Marshal(n)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(state, resumeFile), raw, 0o600))
			d.applyResume(state, time.Now())
			require.False(t, load(t, st).Local["m1"].Mounted)
			require.NoFileExists(t, filepath.Join(state, resumeFile))
		})
	}
}

func TestMountRetriesFastJustAfterStart(t *testing.T) {
	retryBase = time.Hour
	t.Cleanup(func() { retryBase = 0 })
	d, f, _ := newTestDaemon(t, sample("m1"))
	f.failMount = errors.New("the share name is already in use")
	d.start()
	d.mu.Lock()
	wait := time.Until(d.states["m1"].retryAt)
	logged := d.states["m1"].logged
	d.mu.Unlock()
	require.LessOrEqual(t, wait, fastRetry)
	require.False(t, logged, "a drive still settling after a start is not a failure yet")
}

func TestNoRestartWhileInstallRuns(t *testing.T) {
	d, _, _ := newTestDaemon(t, sample())
	require.NoError(t, writeAtomic(d.upd.paths.State, "version", "999.0.0"))
	d.upd.mu.Lock()
	require.False(t, d.autoUpdate(context.Background(), time.Now()), "the selftest is still running")
	d.upd.mu.Unlock()
}
