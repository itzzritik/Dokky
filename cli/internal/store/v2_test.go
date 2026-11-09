package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/vault"
	"github.com/stretchr/testify/require"
)

func accountKey(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func legacyStore(t *testing.T, dir string, kr *fakeKeyring) *Store {
	t.Helper()
	s, err := OpenLegacy(dir, kr)
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))
	return s
}

func requireFormat(t *testing.T, dir string, want int) {
	t.Helper()
	v, err := FormatOf(dir)
	require.NoError(t, err)
	require.Equal(t, want, v)
}

func TestV2RoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))
	requireFormat(t, dir, 2)

	raw, err := os.ReadFile(filepath.Join(dir, "store.enc"))
	require.NoError(t, err)
	require.Equal(t, []byte("DKY2"), raw[:4])
	require.False(t, bytes.Contains(raw, []byte("hunter2-secret")))
	plain, err := vault.OpenBytes(accountKey(1), raw[4:], "dokky/v1/store")
	require.NoError(t, err, "the body is a vault envelope bound to the store")
	require.Contains(t, string(plain), "hunter2-secret")

	s2, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	got, err := s2.Load()
	require.NoError(t, err)
	require.Equal(t, sample(), got)
}

func TestV2WrongKey(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))

	s2, err := OpenWithKey(dir, accountKey(2))
	require.NoError(t, err)
	_, err = s2.Load()
	require.ErrorIs(t, err, errWrongAccKey)

	_, err = OpenWithKey(dir, accountKey(1)[:16])
	require.Error(t, err)
}

func TestOpenWithKeyKeepsItsOwnCopy(t *testing.T) {
	dir := t.TempDir()
	ak := accountKey(1)
	s, err := OpenWithKey(dir, ak)
	require.NoError(t, err)
	vault.Zero(ak)
	require.NoError(t, s.Save(sample()))
	s2, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	_, err = s2.Load()
	require.NoError(t, err)
}

func TestOpenWithKeyOnV1File(t *testing.T) {
	dir := t.TempDir()
	legacyStore(t, dir, &fakeKeyring{})
	s, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	_, err = s.Load()
	require.ErrorIs(t, err, ErrOldFormat)
}

func TestUpgradeFromV1(t *testing.T) {
	dir := t.TempDir()
	kr := &fakeKeyring{}
	s := legacyStore(t, dir, kr)
	requireFormat(t, dir, 1)
	require.NotNil(t, kr.key)

	require.NoError(t, s.Upgrade(accountKey(1)))
	requireFormat(t, dir, 2)
	require.Nil(t, kr.key, "the old keychain key is gone")

	got, err := s.Load()
	require.NoError(t, err)
	require.Equal(t, sample(), got)
	require.NoError(t, s.Save(got))
	requireFormat(t, dir, 2)

	s2, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	got, err = s2.Load()
	require.NoError(t, err)
	require.Equal(t, sample(), got)
}

func TestUpgradeFreshInstall(t *testing.T) {
	dir := t.TempDir()
	kr := &fakeKeyring{}
	s, err := OpenLegacy(dir, kr)
	require.NoError(t, err)
	require.NoError(t, s.Upgrade(accountKey(1)))
	requireFormat(t, dir, 2)
	require.Nil(t, kr.key)
	got, err := s.Load()
	require.NoError(t, err)
	require.Equal(t, Fresh(), got)
}

// the v2 file lands before the old key goes; if the key delete dies, the v2 file still wins and a second
// upgrade finishes the job
func TestUpgradeInterruptedBeforeKeyDelete(t *testing.T) {
	dir := t.TempDir()
	kr := &fakeKeyring{}
	s := legacyStore(t, dir, kr)
	kr.deleteFail = errors.New("keychain locked")

	err := s.Upgrade(accountKey(1))
	require.ErrorContains(t, err, "could not be removed")
	requireFormat(t, dir, 2)
	require.NotNil(t, kr.key)

	again, err := OpenLegacy(dir, kr)
	require.NoError(t, err)
	_, err = again.Load()
	require.ErrorIs(t, err, ErrNeedsKey, "the old key cannot read a v2 store")
	require.ErrorIs(t, again.Save(sample()), ErrNeedsKey, "a v2 store is never overwritten as v1")
	requireFormat(t, dir, 2)

	kr.deleteFail = nil
	require.NoError(t, again.Upgrade(accountKey(1)))
	require.Nil(t, kr.key)
	got, err := again.Load()
	require.NoError(t, err)
	require.Equal(t, sample(), got)
}

// a failed upgrade leaves the v1 store, its key and the store's format exactly as they were
func TestUpgradeFailureChangesNothing(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a read-only directory")
	}
	dir := t.TempDir()
	kr := &fakeKeyring{}
	s := legacyStore(t, dir, kr)
	key := bytes.Clone(kr.key)

	require.Error(t, s.Upgrade(accountKey(1)[:8]))

	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	require.Error(t, s.Upgrade(accountKey(1)))
	require.NoError(t, os.Chmod(dir, 0o700))

	requireFormat(t, dir, 1)
	require.Equal(t, key, kr.key)
	require.NoError(t, s.Save(sample()))
	requireFormat(t, dir, 1)
	got, err := s.Load()
	require.NoError(t, err)
	require.Equal(t, sample(), got)
}

func TestUpgradeNeedsTheOldKeyForV1(t *testing.T) {
	dir := t.TempDir()
	legacyStore(t, dir, &fakeKeyring{})
	s, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	require.ErrorIs(t, s.Upgrade(accountKey(1)), ErrOldFormat)
	requireFormat(t, dir, 1)
}

func TestRekey(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))

	require.NoError(t, s.Rekey(accountKey(2)))
	got, err := s.Load()
	require.NoError(t, err)
	require.Equal(t, sample(), got)
	require.FileExists(t, filepath.Join(dir, "store.enc.prev"))

	// vault.json now says key 2: the next start keeps the new file and drops the old one
	s2, err := OpenWithKey(dir, accountKey(2))
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(dir, "store.enc.prev"))
	got, err = s2.Load()
	require.NoError(t, err)
	require.Equal(t, sample(), got)

	s1, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	_, err = s1.Load()
	require.ErrorIs(t, err, errWrongAccKey)
}

// a crash after the rekey but before vault.json switched keys: the next start, still on key 1, gets the old file back
func TestRekeyInterruptedBeforeVaultSwitch(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))
	require.NoError(t, s.Rekey(accountKey(2)))

	s1, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(dir, "store.enc.prev"))
	got, err := s1.Load()
	require.NoError(t, err)
	require.Equal(t, sample(), got)
}

func TestRekeyOnLegacyStoreUpgrades(t *testing.T) {
	dir := t.TempDir()
	kr := &fakeKeyring{}
	s := legacyStore(t, dir, kr)
	require.NoError(t, s.Rekey(accountKey(3)))
	requireFormat(t, dir, 2)
	require.Nil(t, kr.key)
	s2, err := OpenWithKey(dir, accountKey(3))
	require.NoError(t, err)
	got, err := s2.Load()
	require.NoError(t, err)
	require.Equal(t, sample(), got)
}

func TestCloseWipesKey(t *testing.T) {
	dir := t.TempDir()
	ak := accountKey(1)
	s, err := OpenWithKey(dir, ak)
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))
	s.Close()
	_, err = s.Load()
	require.ErrorIs(t, err, ErrNeedsKey)
	require.ErrorIs(t, s.Save(sample()), ErrNeedsKey)
	requireFormat(t, dir, 2)
}

func TestFormatOf(t *testing.T) {
	dir := t.TempDir()
	requireFormat(t, dir, 0)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "store.enc"), []byte("DK"), 0o600))
	requireFormat(t, dir, 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "store.enc"), []byte("DKY2 and the rest"), 0o600))
	requireFormat(t, dir, 2)
}

func TestDeleteV2(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))
	require.NoError(t, s.Rekey(accountKey(2)))
	require.NoError(t, s.Delete())
	require.NoFileExists(t, filepath.Join(dir, "store.enc"))
	require.NoFileExists(t, filepath.Join(dir, "store.enc.prev"))

	got, err := s.Load()
	require.NoError(t, err)
	require.Empty(t, got.Services)
	require.NoError(t, s.Save(got))
	requireFormat(t, dir, 2)
}

// a lost store.enc next to the old file: the old file comes back rather than being dropped for an empty store
func TestSettleRestoresWhenStoreIsGone(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	require.NoError(t, s.Save(sample()))
	require.NoError(t, s.Rekey(accountKey(2)))
	require.NoError(t, os.Remove(filepath.Join(dir, "store.enc")))

	s1, err := OpenWithKey(dir, accountKey(1))
	require.NoError(t, err)
	got, err := s1.Load()
	require.NoError(t, err)
	require.Equal(t, sample(), got)
}
