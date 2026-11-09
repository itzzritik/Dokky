package vault

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 11, 9, 2, 0, 0, 0, time.UTC)

func newFile(t *testing.T) (*File, []byte) {
	t.Helper()
	f, ak, _, err := Create([]byte("correct horse battery"), key(5), now)
	require.NoError(t, err)
	return f, ak
}

func TestFileSaveLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	f, _ := newFile(t)
	require.NoError(t, f.Save(dir))

	got, err := Load(dir)
	require.NoError(t, err)
	require.Equal(t, f, got)

	info, err := os.Stat(filepath.Join(dir, "vault.json"))
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	require.NoError(t, f.Save(dir))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temp files are left behind")
}

func TestFileJSONMatchesSpec(t *testing.T) {
	dir := t.TempDir()
	f, _ := newFile(t)
	require.NoError(t, f.Save(dir))
	raw, err := os.ReadFile(filepath.Join(dir, "vault.json"))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	require.ElementsMatch(t, []string{
		"version", "keyId", "kdf", "wrappedKey", "recoveryWrappedKey", "deviceWrappedKey",
		"unlock", "passwordDays", "idleLockMinutes", "passwordAt", "createdAt", "account",
	}, keys(m))
	require.Equal(t, "2026-11-09T02:00:00Z", m["passwordAt"])
	require.Equal(t, "auto", m["unlock"])
	require.InDelta(t, 14, m["passwordDays"], 0)
	require.InDelta(t, 10, m["idleLockMinutes"], 0)
	require.Equal(t, false, m["account"])
}

func TestLoadMissing(t *testing.T) {
	_, err := Load(t.TempDir())
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestLoadRejectsBadFiles(t *testing.T) {
	cases := map[string]func(f *File){
		"newer version":  func(f *File) { f.Version = 2 },
		"no version":     func(f *File) { f.Version = 0 },
		"short key id":   func(f *File) { f.KeyID = "abc" },
		"upper key id":   func(f *File) { f.KeyID = "C36F75F54394483A" },
		"hostile memory": func(f *File) { f.KDF.M = 1 << 30 },
		"hostile time":   func(f *File) { f.KDF.T = 1000 },
		"other kdf":      func(f *File) { f.KDF.Alg = "pbkdf2" },
		"unlock":         func(f *File) { f.Unlock = "sometimes" },
		"empty unlock":   func(f *File) { f.Unlock = "" },
		"password days":  func(f *File) { f.PasswordDays = 3 },
		"idle minutes":   func(f *File) { f.IdleLockMinutes = 15 },
		"no wrapped key": func(f *File) { f.WrappedKey = "" },
		"no recovery":    func(f *File) { f.RecoveryWrappedKey = "" },
		"garbage wrap":   func(f *File) { f.WrappedKey = "!!!" },
		"short wrap":     func(f *File) { f.DeviceWrappedKey = "AQID" },
		"huge wrap": func(f *File) {
			f.RecoveryWrappedKey = base64.StdEncoding.EncodeToString(append([]byte{1}, make([]byte, 600)...))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			f, _ := newFile(t)
			mutate(f)
			require.Error(t, f.Validate())
			require.Error(t, f.Save(dir), "an invalid vault is never written")

			b, err := json.Marshal(f)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), b, 0o600))
			_, err = Load(dir)
			require.ErrorContains(t, err, "vault.json")
		})
	}

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), []byte("{not json"), 0o600))
	_, err := Load(dir)
	require.ErrorContains(t, err, "damaged")
}

func TestFileWithoutDeviceWrapIsValid(t *testing.T) {
	f, _ := newFile(t)
	f.DeviceWrappedKey = ""
	f.Unlock = UnlockPassword
	require.NoError(t, f.Validate())
}

func TestPasswordDue(t *testing.T) {
	f, _ := newFile(t)
	f.PasswordDays = 14
	require.False(t, f.PasswordDue(now.Add(13*24*time.Hour)))
	require.True(t, f.PasswordDue(now.Add(14*24*time.Hour)))
	f.PasswordDays = 0
	require.False(t, f.PasswordDue(now.Add(1000*24*time.Hour)))
}

func TestCloneIsDeep(t *testing.T) {
	f, _ := newFile(t)
	c := f.Clone()
	c.KDF.Salt[0] ^= 0xff
	c.Unlock = UnlockSystem
	require.NotEqual(t, f.KDF.Salt[0], c.KDF.Salt[0])
	require.Equal(t, UnlockAuto, f.Unlock)
}

// the daemon speaks api's names; vault must agree with them
func TestNamesMatchAPI(t *testing.T) {
	require.Equal(t, api.UnlockAuto, UnlockAuto)
	require.Equal(t, api.UnlockSystem, UnlockSystem)
	require.Equal(t, api.UnlockPassword, UnlockPassword)
	require.Equal(t, api.PasswordDayChoices, PasswordDayChoices)
	require.Equal(t, api.IdleLockChoices, IdleLockChoices)
}
