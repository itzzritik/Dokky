package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeKeyring struct {
	key  []byte
	fail error
}

func (f *fakeKeyring) Get() ([]byte, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	if f.key == nil {
		return nil, ErrNotFound
	}
	return f.key, nil
}

func (f *fakeKeyring) Set(k []byte) error {
	if f.fail != nil {
		return f.fail
	}
	f.key = k
	return nil
}

func (f *fakeKeyring) Delete() error {
	f.key = nil
	return nil
}

func sample() Data {
	return Data{
		Services: []Service{{
			ID: "s1", Name: "nas", Type: "sftp",
			Options:   map[string]string{"host": "nas.local"},
			Secrets:   map[string]string{"pass": "hunter2-secret"},
			Version:   3,
			UpdatedAt: time.Date(2026, 10, 30, 1, 2, 3, 0, time.UTC),
		}},
		Mounts: []Mount{{ID: "m1", Name: "media", ServiceID: "s1", RemotePath: "/ssd", VFS: DefaultVFS(), Version: 1}},
		Local:  map[string]LocalMount{"m1": {MountPoint: "/home/u/dokky/media", Mounted: true}},
		Account: &Account{
			APIBase: "https://dokky.ritik.me", DeviceID: "d1", Token: "dky_abc", Rev: "r1",
		},
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	kr := &fakeKeyring{}
	s, err := Open(dir, kr)
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))

	s2, err := Open(dir, kr)
	require.NoError(t, err)
	got, err := s2.Load()
	require.NoError(t, err)
	require.Equal(t, sample(), got)
}

func TestLoadMissingIsEmpty(t *testing.T) {
	s, err := Open(t.TempDir(), &fakeKeyring{})
	require.NoError(t, err)
	got, err := s.Load()
	require.NoError(t, err)
	require.NotNil(t, got.Local)
	require.Empty(t, got.Services)
	require.True(t, got.Settings.AutoUpdate)
}

func TestFileIsNotPlaintext(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, &fakeKeyring{})
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))
	raw, err := os.ReadFile(filepath.Join(dir, "store.enc"))
	require.NoError(t, err)
	require.False(t, bytes.Contains(raw, []byte("hunter2-secret")))
	require.False(t, bytes.Contains(raw, []byte("dky_abc")))
}

func TestWrongKeyFails(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, &fakeKeyring{})
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))

	s2, err := Open(dir, &fakeKeyring{key: bytes.Repeat([]byte{1}, 32)})
	require.NoError(t, err)
	_, err = s2.Load()
	require.Error(t, err)
}

func TestAtomicSaveLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, &fakeKeyring{})
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))
	require.NoError(t, s.Save(sample()))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.Equal(t, "store.enc", e.Name())
	}
}

func TestDeleteRemovesFileAndKey(t *testing.T) {
	dir := t.TempDir()
	kr := &fakeKeyring{}
	s, err := Open(dir, kr)
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))
	require.NoError(t, s.Delete())
	require.NoFileExists(t, filepath.Join(dir, "store.enc"))
	require.Nil(t, kr.key)
	require.NoError(t, s.Delete())
}

func TestSaveAfterDeleteStartsNewKey(t *testing.T) {
	dir := t.TempDir()
	kr := &fakeKeyring{}
	s, err := Open(dir, kr)
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))
	require.NoError(t, s.Delete())
	got, err := s.Load()
	require.NoError(t, err)
	require.Empty(t, got.Services)

	fresh := Data{Local: map[string]LocalMount{}, Services: []Service{{ID: "n1", Name: "fresh"}}}
	require.NoError(t, s.Save(fresh))
	require.NotNil(t, kr.key)
	s2, err := Open(dir, kr)
	require.NoError(t, err)
	got, err = s2.Load()
	require.NoError(t, err)
	require.Equal(t, fresh, got)
}

func TestFallbackKeyFileMode0600(t *testing.T) {
	dir := t.TempDir()
	kr := newKeyring(&fakeKeyring{fail: errors.New("no secret service")}, dir, true)
	s, err := Open(dir, kr)
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))
	require.True(t, kr.Fallback())

	path := filepath.Join(dir, "store.key")
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	s2, err := Open(dir, newKeyring(&fakeKeyring{fail: errors.New("no secret service")}, dir, true))
	require.NoError(t, err)
	got, err := s2.Load()
	require.NoError(t, err)
	require.Equal(t, sample(), got)

	require.NoError(t, s2.Delete())
	require.NoFileExists(t, path)
}

func TestNoFallbackWhenDisallowed(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(dir, newKeyring(&fakeKeyring{fail: errors.New("boom")}, dir, false))
	require.Error(t, err)
	require.NoFileExists(t, filepath.Join(dir, "store.key"))
}

func TestDefaultVFSValues(t *testing.T) {
	require.Equal(t, VFS{
		CacheMode:         "full",
		DirCacheTime:      "30s",
		ReadAhead:         "2G",
		ReadChunkStreams:  4,
		CacheMaxSize:      "50G",
		CacheMaxAge:       "5m",
		CachePollInterval: "10s",
		CacheMinFreeSpace: "10G",
		FastFingerprint:   true,
	}, DefaultVFS())
}

func TestOpenRefusesWhenKeyMissingButStoreExists(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, &fakeKeyring{})
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))

	kr := &fakeKeyring{}
	_, err = Open(dir, kr)
	require.ErrorIs(t, err, ErrKeyMissing)
	require.ErrorContains(t, err, filepath.Join(dir, "store.enc"))
	require.Nil(t, kr.key)
}

func TestKeyringEnvUsesFile(t *testing.T) {
	t.Setenv("DOKKY_KEYRING", "file")
	dir := t.TempDir()
	kr := SystemKeyring(dir)
	require.IsType(t, fileOnly{}, kr.(*fileFallbackKeyring).primary)

	s, err := Open(dir, kr)
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))
	require.FileExists(t, filepath.Join(dir, "store.key"))
	require.True(t, kr.(Fallbacker).Fallback())
}
