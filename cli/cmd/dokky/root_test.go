package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/platform"

	"github.com/stretchr/testify/require"
)

func TestVersionFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("PATH", "/usr/bin:/bin")
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})
	require.NoError(t, cmd.Execute())
	require.Equal(t, "dokky 0.0.0-dev\n", out.String())
}

func cliHome(t *testing.T) {
	t.Helper()
	root := "/tmp"
	if runtime.GOOS == "windows" {
		root = ""
	}
	home, err := os.MkdirTemp(root, "dk")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("LocalAppData", filepath.Join(home, "AppData", "Local"))
}

func TestCallWithoutDaemonOrBinary(t *testing.T) {
	cliHome(t)
	require.ErrorContains(t, call("status", nil, nil), "npm i -g dokky")
}

func fakeDaemon(t *testing.T, handlers map[string]ipc.Handler) {
	t.Helper()
	cliHome(t)
	state, err := platform.StateDir()
	require.NoError(t, err)
	ln, err := platform.Listen(platform.SocketAddr(state))
	require.NoError(t, err)
	srv := ipc.NewServer(ln, handlers)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func TestCallRestartsOnProtocolMismatch(t *testing.T) {
	var calls, restarts atomic.Int32
	fakeDaemon(t, map[string]ipc.Handler{
		"status": func(context.Context, json.RawMessage) (any, error) {
			if calls.Add(1) == 1 {
				return nil, &ipc.Error{Code: "protocol_mismatch", Message: "old daemon"}
			}
			return map[string]string{"version": "new"}, nil
		},
		"daemon.restart": func(context.Context, json.RawMessage) (any, error) {
			restarts.Add(1)
			return nil, nil
		},
	})

	var out map[string]string
	require.NoError(t, call("status", nil, &out))
	require.Equal(t, "new", out["version"])
	require.EqualValues(t, 1, restarts.Load())
}
