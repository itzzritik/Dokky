package sysauth

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeRun answers every helper with res and err and records what would have started
func fakeRun(t *testing.T, res result, err error) *[]command {
	t.Helper()
	var calls []command
	old := run
	run = func(_ context.Context, c command) (result, error) {
		calls = append(calls, c)
		return res, err
	}
	t.Cleanup(func() { run = old })
	return &calls
}

func fakeEnv(t *testing.T, env map[string]string, pkexec bool) {
	t.Helper()
	oldEnv, oldLook := getenv, lookPath
	getenv = func(k string) string { return env[k] }
	lookPath = func(name string) (string, error) {
		if pkexec && name == "pkexec" {
			return "/usr/bin/pkexec", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { getenv, lookPath = oldEnv, oldLook })
}

func TestRemoteIsNeverAvailable(t *testing.T) {
	calls := fakeRun(t, result{out: "touchid"}, nil)
	fakeEnv(t, map[string]string{"DISPLAY": ":0"}, true)
	name, ok := Available(true)
	require.False(t, ok)
	require.Empty(t, name)
	require.Empty(t, *calls)
}

func TestDaemonOverSSHIsUnavailable(t *testing.T) {
	calls := fakeRun(t, result{out: "ok"}, nil)
	fakeEnv(t, map[string]string{"DISPLAY": ":0", "SSH_CONNECTION": "10.0.0.2 51000 10.0.0.1 22"}, true)
	_, ok := Available(false)
	require.False(t, ok)
	require.ErrorIs(t, Prompt(context.Background(), "unlock dokky"), ErrUnavailable)
	require.Empty(t, *calls)
}

func TestPromptOnThisOS(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("no system prompt on " + runtime.GOOS)
	}
	calls := fakeRun(t, result{out: "ok", code: 0}, nil)
	fakeEnv(t, map[string]string{"DISPLAY": ":0"}, true)
	require.NoError(t, Prompt(context.Background(), "unlock dokky to mount your drives"))
	require.Len(t, *calls, 1)

	fakeRun(t, result{code: -1}, context.Canceled)
	require.ErrorIs(t, Prompt(context.Background(), ""), ErrCancelled)
}

func TestAvailabilityIsCached(t *testing.T) {
	checks := 0
	c := &cache{check: func(context.Context) (string, bool) {
		checks++
		return "Touch ID", true
	}}
	at := time.Date(2026, 11, 9, 2, 0, 0, 0, time.UTC)
	old := clock
	clock = func() time.Time { return at }
	t.Cleanup(func() { clock = old })

	for range 5 {
		name, ok := c.get()
		require.True(t, ok)
		require.Equal(t, "Touch ID", name)
	}
	require.Equal(t, 1, checks)
	at = at.Add(cacheFor)
	c.get()
	require.Equal(t, 2, checks)
}

func TestExecRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	res, err := execRun(context.Background(), command{path: "/bin/sh", args: []string{"-c", `echo "$DOKKY_T"; echo oops >&2; exit 3`}, env: []string{"DOKKY_T=hi"}})
	require.NoError(t, err, "a non-zero exit is a result")
	require.Equal(t, result{out: "hi", stderr: "oops", code: 3}, res)

	_, err = execRun(context.Background(), command{path: "/no/such/helper"})
	require.ErrorIs(t, err, os.ErrNotExist)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = execRun(ctx, command{path: "/bin/sh", args: []string{"-c", "sleep 30"}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 5*time.Second, "a stopped prompt kills its helper")
}
