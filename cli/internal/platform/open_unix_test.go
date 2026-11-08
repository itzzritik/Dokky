//go:build !windows

package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenURLUsesBrowserEnv(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "opened")
	script := filepath.Join(dir, "browser.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nprintf %s \"$1\" > "+out+"\n"), 0o755))
	t.Setenv("BROWSER", script)
	require.NoError(t, OpenURL("https://dokky.ritik.me/device"))
	got, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Equal(t, "https://dokky.ritik.me/device", string(got))
}
