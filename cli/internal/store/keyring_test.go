package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceKeyringIsSeparate(t *testing.T) {
	t.Setenv("DOKKY_KEYRING", "file")
	dir := t.TempDir()
	device, legacy := DeviceKeyring(dir), SystemKeyring(dir)

	key, err := EnsureKey(device)
	require.NoError(t, err)
	require.Len(t, key, 32)
	require.FileExists(t, filepath.Join(dir, "device.key"))
	require.NoFileExists(t, filepath.Join(dir, "store.key"))
	_, err = legacy.Get()
	require.ErrorIs(t, err, ErrNotFound)

	again, err := EnsureKey(DeviceKeyring(dir))
	require.NoError(t, err)
	require.Equal(t, key, again, "the key is made once")

	require.NoError(t, device.Delete())
	require.NoFileExists(t, filepath.Join(dir, "device.key"))
}

func TestEnsureKeyFallsBackToFile(t *testing.T) {
	dir := t.TempDir()
	kr := newKeyring(&fakeKeyring{fail: errors.New("no secret service")}, filepath.Join(dir, "device.key"), true)
	key, err := EnsureKey(kr)
	require.NoError(t, err)
	require.True(t, kr.Fallback())
	got, err := kr.Get()
	require.NoError(t, err)
	require.Equal(t, key, got)
}

func TestEnsureKeyErrors(t *testing.T) {
	_, err := EnsureKey(&fakeKeyring{key: []byte("short")})
	require.ErrorContains(t, err, "wrong length")

	dir := t.TempDir()
	_, err = EnsureKey(newKeyring(&fakeKeyring{fail: errors.New("boom")}, filepath.Join(dir, "device.key"), false))
	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(dir, "device.key"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}
