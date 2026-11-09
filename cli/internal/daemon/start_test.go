package daemon

import (
	"log/slog"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/stretchr/testify/require"
)

// the daemon's start without a state folder: services in, then the flagged drives up
func (d *Daemon) start() {
	d.loadServices()
	d.remountAtStart("")
}

// the next daemon on the same store, as after a restart
func newTestDaemonOn(t *testing.T, st *store.Store) (*Daemon, *fakeEngine, *store.Store) {
	t.Helper()
	data, err := st.Load()
	require.NoError(t, err)
	f := newFakeEngine()
	d := newDaemon(f, st, data, t.TempDir(), t.TempDir(), slog.New(slog.DiscardHandler), unlockedKeeper(t, t.TempDir(), false))
	d.isMounted, d.forceUnmount = f.isMounted, f.forceUnmount
	d.upd.paths.State = t.TempDir()
	return d, f, st
}
