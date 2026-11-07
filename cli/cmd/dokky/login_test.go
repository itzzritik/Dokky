package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/stretchr/testify/require"
)

func TestLoginPromptsForEachClash(t *testing.T) {
	var resolved []map[string]string
	fakeDaemon(t, map[string]ipc.Handler{
		"login.start": func(context.Context, json.RawMessage) (any, error) {
			return map[string]any{"userCode": "BCDF-GHJK", "verifyUrl": "https://dokky.ritik.me/cli/approve?code=BCDF-GHJK", "interval": 2}, nil
		},
		"login.poll": func(context.Context, json.RawMessage) (any, error) {
			return map[string]any{"status": "approved", "clashes": []map[string]string{{"kind": "service", "name": "nas", "localId": "s1", "remoteId": "r1", "local": "sftp, port=22, host set", "account": "webdav, url=http://nas"}}}, nil
		},
		"login.resolve": func(_ context.Context, raw json.RawMessage) (any, error) {
			var p map[string]string
			require.NoError(t, json.Unmarshal(raw, &p))
			resolved = append(resolved, p)
			switch len(resolved) {
			case 1:
				return nil, &ipc.Error{Code: "internal", Message: "2 uploads on media still running; resolve again when they finish"}
			case 2:
				return map[string]any{"clashes": []map[string]string{{"kind": "mount", "name": "media", "localId": "m1", "remoteId": "rm1"}}}, nil
			}
			return map[string]any{"clashes": []any{}}, nil
		},
	})

	var out, errOut bytes.Buffer
	cmd := newLoginCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader("x\naccount\naccount\nl\n"))
	cmd.SetArgs(nil)
	require.NoError(t, cmd.Execute())
	require.Equal(t, []map[string]string{
		{"kind": "service", "name": "nas", "keep": "account"},
		{"kind": "service", "name": "nas", "keep": "account"},
		{"kind": "mount", "name": "media", "keep": "local"},
	}, resolved)
	require.Contains(t, out.String(), "local:   sftp, port=22, host set")
	require.Contains(t, out.String(), "account: webdav, url=http://nas")
	require.Contains(t, errOut.String(), "2 uploads on media still running")
	require.Contains(t, out.String(), "BCDF-GHJK")
	require.Contains(t, out.String(), "https://dokky.ritik.me/cli/approve?code=BCDF-GHJK")
	require.Contains(t, out.String(), "Logged in")
}

func TestLoginStopsOnDenied(t *testing.T) {
	fakeDaemon(t, map[string]ipc.Handler{
		"login.start": func(context.Context, json.RawMessage) (any, error) {
			return map[string]any{"userCode": "BCDF-GHJK", "verifyUrl": "https://x", "interval": 2}, nil
		},
		"login.poll": func(context.Context, json.RawMessage) (any, error) {
			return map[string]any{"status": "denied"}, nil
		},
	})
	cmd := newLoginCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(nil)
	require.ErrorContains(t, cmd.Execute(), "denied")
}

func TestLogoutForceWarnsAboutUploads(t *testing.T) {
	var forced bool
	fakeDaemon(t, map[string]ipc.Handler{
		"status": func(context.Context, json.RawMessage) (any, error) {
			return map[string]any{"loggedIn": true, "mounts": []map[string]any{{"name": "media", "uploadsQueued": 2, "uploadsInProgress": 1}}}, nil
		},
		"logout": func(_ context.Context, raw json.RawMessage) (any, error) {
			var p map[string]bool
			require.NoError(t, json.Unmarshal(raw, &p))
			forced = p["force"]
			return map[string]string{}, nil
		},
	})
	var out, errOut bytes.Buffer
	cmd := newLogoutCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--force"})
	require.NoError(t, cmd.Execute())
	require.True(t, forced)
	require.Contains(t, errOut.String(), "3 unsent uploads will be discarded")
	require.Contains(t, out.String(), "Removed this device.")
}
