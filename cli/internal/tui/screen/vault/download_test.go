package vault

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSaveKeyNeverOverwritesAnotherFile(t *testing.T) {
	dir := t.TempDir()
	first, err := writeNew(dir, []byte("one"))
	require.NoError(t, err)
	second, err := writeNew(dir, []byte("two"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "dokky-recovery-key.txt"), first)
	require.Equal(t, filepath.Join(dir, "dokky-recovery-key (2).txt"), second)
	b, _ := os.ReadFile(first)
	require.Equal(t, "one", string(b), "the older key file is left alone")
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(second)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "only the person can read it")
	}
}

func TestKeyText(t *testing.T) {
	at := time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC)
	s := keyText("DK1-ABCD", "you@example.com", at)
	require.Contains(t, s, "DK1-ABCD")
	require.Contains(t, s, "Account: you@example.com")
	require.Contains(t, s, "Created: 9 November 2026")
	require.NotContains(t, keyText("DK1-ABCD", "", at), "Account:")
}
