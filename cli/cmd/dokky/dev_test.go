package main

import (
	"bytes"
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/stretchr/testify/require"
)

// a daemon reporting version, which exits when asked to restart
func runningDaemon(t *testing.T, version string) *atomic.Int32 {
	t.Helper()
	cliHome(t)
	state, err := platform.StateDir()
	require.NoError(t, err)
	ln, err := platform.Listen(platform.SocketAddr(state))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	var restarts atomic.Int32
	srv := ipc.NewServer(ln, map[string]ipc.Handler{
		api.MState: func(context.Context, json.RawMessage) (any, error) { return api.State{Version: version}, nil },
		"daemon.restart": func(context.Context, json.RawMessage) (any, error) {
			restarts.Add(1)
			go cancel()
			return nil, nil
		},
	})
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return &restarts
}

func TestInstalledDokkyTakesTheServiceBackFromADevBuild(t *testing.T) {
	restarts := runningDaemon(t, "0.0.0-dev")
	var note bytes.Buffer
	err := useInstalledDaemon(&note)
	require.EqualValues(t, 1, restarts.Load(), "the dev build's daemon is asked to exit")
	require.ErrorContains(t, err, "npm i -g dokky", "then the installed service starts; this home has none")
	require.Contains(t, note.String(), "switching back to the installed background service")
}

func TestInstalledDokkyKeepsAnInstalledService(t *testing.T) {
	restarts := runningDaemon(t, "0.1.34")
	var note bytes.Buffer
	require.NoError(t, useInstalledDaemon(&note))
	require.Zero(t, restarts.Load(), "another installed version is Health's to restart, once nothing uploads")
	require.Empty(t, note.String())
}

func TestInstalledDokkyWithNoServiceRunning(t *testing.T) {
	cliHome(t)
	require.NoError(t, useInstalledDaemon(&bytes.Buffer{}), "the app starts the service itself as it opens")
}
