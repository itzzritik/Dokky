package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/stretchr/testify/require"
)

func TestUpdateAsksWhileUploadsQueued(t *testing.T) {
	var mu sync.Mutex
	var asked []map[string]string
	fakeDaemon(t, map[string]ipc.Handler{
		"status": func(context.Context, json.RawMessage) (any, error) {
			return daemon.StatusReply{Mounts: []daemon.MountStatus{{Name: "media", UploadsQueued: 2}}}, nil
		},
		"update": func(_ context.Context, raw json.RawMessage) (any, error) {
			var p map[string]string
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			mu.Lock()
			asked = append(asked, p)
			mu.Unlock()
			return map[string]any{"version": "0.2.0", "updated": true}, nil
		},
	})
	run := func(stdin string, args ...string) string {
		var out bytes.Buffer
		cmd := newRootCmd()
		cmd.SetIn(strings.NewReader(stdin))
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append([]string{"update"}, args...))
		require.NoError(t, cmd.Execute())
		return out.String()
	}

	require.Contains(t, run("n\n"), "Not updated")
	require.Empty(t, asked)
	require.Contains(t, run("y\n", "--to", "0.0.9"), "0.2.0")
	require.Contains(t, run("", "--yes"), "0.2.0")
	require.Equal(t, []map[string]string{{"to": "0.0.9"}, {"to": ""}}, asked)
}
