package logx

import (
	"context"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/librclone/librclone"
	"github.com/stretchr/testify/require"
)

func newTestLogger(t *testing.T) (*slog.Logger, string) {
	t.Helper()
	ctx := context.Background()
	prev := fs.GetConfig(ctx).LogLevel
	prevDefault := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(prevDefault)
		fs.GetConfig(ctx).LogLevel = prev
		fs.SetLogger(slog.Default().Handler())
		SetSecrets(nil)
	})
	path := filepath.Join(t.TempDir(), "dokky.log")
	return New(path), path
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func TestLogRedactsPlainAndObscured(t *testing.T) {
	log, path := newTestLogger(t)
	SetSecrets([]string{"hunter2"})
	obscured := obscure.MustObscure("hunter2")

	log.Error("dial failed pass=hunter2", "detail", "x "+obscure.MustObscure("hunter2"), slog.Group("g", "p", "hunter2"))
	log.Error("rejected " + obscured)

	out := readLog(t, path)
	require.NotContains(t, out, "hunter2")
	require.NotContains(t, out, obscured)
	require.Contains(t, out, "***")
}

func TestShortSecretOnlyRedactedWhenObscured(t *testing.T) {
	log, path := newTestLogger(t)
	SetSecrets([]string{"nas"})
	obscured := obscure.MustObscure("nas")

	log.Error("connecting to nas " + obscured)

	out := readLog(t, path)
	require.Contains(t, out, "connecting to nas")
	require.NotContains(t, out, obscured)
}

func TestRcloneLogsRedactedAndNotDebug(t *testing.T) {
	_, path := newTestLogger(t)
	SetSecrets([]string{"hunter2"})

	require.Equal(t, fs.LogLevelNotice, fs.GetConfig(context.Background()).LogLevel)
	fs.Errorf(nil, "login failed pass=hunter2")
	fs.Debugf(nil, "debug line")

	out := readLog(t, path)
	require.NotContains(t, out, "hunter2")
	require.NotContains(t, out, "debug line")
	require.Contains(t, out, "***")
}

type payload struct {
	Pass string
	Ptr  *string
}

func TestRedactsNonStringAttrs(t *testing.T) {
	log, path := newTestLogger(t)
	SetSecrets([]string{"hunter2"})
	pw := "hunter2"

	log.Error("x",
		"list", []string{"a", "hunter2"},
		"m", map[string]string{"k": "hunter2"},
		"s", payload{Pass: "hunter2", Ptr: &pw},
		"errs", []error{io.ErrUnexpectedEOF, os.ErrNotExist, errString("hunter2")},
	)

	out := readLog(t, path)
	require.NotContains(t, out, "hunter2")
	require.Contains(t, out, "***")
}

type errString string

func (e errString) Error() string { return string(e) }

func TestRcloneInitializeThenNewRedactsDefaultLoggers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RCLONE_CONFIG", filepath.Join(t.TempDir(), "rclone.conf"))
	stdlibFlags, stdlibOut := log.Flags(), log.Writer()
	t.Cleanup(func() { log.SetFlags(stdlibFlags); log.SetOutput(stdlibOut) })

	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	librclone.Initialize()
	_, path := newTestLogger(t)
	SetSecrets([]string{"hunter2"})

	log.Printf("stdlib pass=hunter2")
	slog.Info("default pass=hunter2")

	require.NoError(t, w.Close())
	os.Stderr = origStderr
	stderr, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Empty(t, string(stderr))

	out := readLog(t, path)
	require.NotContains(t, out, "hunter2")
	require.Contains(t, out, "stdlib pass=***")
	require.Contains(t, out, "default pass=***")
}

func TestRedactExtraLongestFirst(t *testing.T) {
	SetSecrets(nil)
	t.Cleanup(func() { SetSecrets(nil) })
	require.Equal(t, "login *** refused for ***", Redact("login ritik@123 refused for ritik", "ritik", "ritik@123"))
}
