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
		"passwordDays", "passwordAt", "createdAt", "account",
	}, keys(m))
	require.Equal(t, "2026-11-09T02:00:00Z", m["passwordAt"])
	require.InDelta(t, 7, m["passwordDays"], 0)
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
	require.NoError(t, f.Validate())
}

func TestPasswordDue(t *testing.T) {
	day := 24 * time.Hour
	cases := map[string]struct {
		days     int
		system   time.Duration // after the typed password; 0 for never
		once     bool
		at       time.Duration // when it is asked, after the typed password
		password time.Duration // the typed password's time moved by this, to put it in the future
		due      bool
	}{
		"inside the interval":                       {days: 7, at: 6 * day},
		"the interval passed":                       {days: 7, at: 7 * day, due: true},
		"system auth slides it":                     {days: 7, system: 5 * day, at: 11 * day},
		"a full interval after the system auth":     {days: 7, system: 5 * day, at: 12 * day, due: true},
		"thirty days":                               {days: 30, at: 29 * day},
		"sliding stops at ninety days":              {days: 7, system: 88 * day, at: 90 * day, due: true},
		"just under ninety days":                    {days: 30, system: 80 * day, at: 89 * day},
		"after a password change":                   {days: 30, once: true, at: time.Minute, due: true},
		"a typed password from the future":          {days: 7, password: 2 * time.Hour, due: true},
		"a little ahead is a clock out of step":     {days: 7, password: 30 * time.Minute},
		"a system auth from the future":             {days: 7, system: 3 * time.Hour, due: true},
		"a system auth a little ahead still slides": {days: 7, system: 30 * time.Minute},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f, _ := newFile(t)
			f.PasswordDays, f.PasswordOnce, f.PasswordAt = c.days, c.once, now.Add(c.password)
			if c.system != 0 {
				f.SystemAt = now.Add(c.system)
			}
			require.Equal(t, c.due, f.PasswordDue(now.Add(c.at)))
		})
	}
}

func TestTypedPasswordRestartsTheWindow(t *testing.T) {
	f, _ := newFile(t)
	f.PasswordOnce = true
	later := now.Add(100 * 24 * time.Hour)
	require.True(t, f.PasswordDue(later))
	f.Typed(later)
	require.False(t, f.PasswordDue(later))
	require.False(t, f.PasswordOnce)
	require.Equal(t, later, f.PasswordAt)
}

func TestSystemStampsOnceAnHour(t *testing.T) {
	f, _ := newFile(t)
	require.True(t, f.StampSystem(now), "the first one saves")
	require.False(t, f.StampSystem(now.Add(59*time.Minute)))
	require.Equal(t, now, f.SystemAt)
	require.True(t, f.StampSystem(now.Add(time.Hour)))
	require.Equal(t, now.Add(time.Hour), f.SystemAt)
	require.True(t, f.StampSystem(now), "a stamp ahead of the clock is put right")
}

// what an older dokky wrote still opens: the unlock and idle lock settings are dropped and the days move to
// today's choices
func TestOldFilesLoad(t *testing.T) {
	cases := map[string]struct {
		days int
		want int
	}{
		"seven stays":         {7, 7},
		"fourteen is fifteen": {14, 15},
		"never is thirty":     {0, 30},
		"thirty stays":        {30, 30},
		"a newer choice":      {60, 15},
		"a broken one":        {-3, 15},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			f, _ := newFile(t)
			b, err := json.Marshal(f)
			require.NoError(t, err)
			var m map[string]any
			require.NoError(t, json.Unmarshal(b, &m))
			m["unlock"], m["idleLockMinutes"], m["passwordDays"] = "password", 30, c.days
			b, err = json.Marshal(m)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), b, 0o600))

			got, err := Load(dir)
			require.NoError(t, err)
			require.Equal(t, c.want, got.PasswordDays)
			require.Equal(t, f.WrappedKey, got.WrappedKey)
			require.False(t, got.PasswordDue(now), "nothing is due because of the move")

			require.NoError(t, got.Save(dir))
			raw, err := os.ReadFile(filepath.Join(dir, FileName))
			require.NoError(t, err)
			require.NotContains(t, string(raw), "unlock")
			require.NotContains(t, string(raw), "idleLockMinutes")
		})
	}
}

func TestCloneIsDeep(t *testing.T) {
	f, _ := newFile(t)
	c := f.Clone()
	c.KDF.Salt[0] ^= 0xff
	c.PasswordDays = 30
	require.NotEqual(t, f.KDF.Salt[0], c.KDF.Salt[0])
	require.Equal(t, 7, f.PasswordDays)
}

// the daemon speaks api's names; vault must agree with them
func TestNamesMatchAPI(t *testing.T) {
	require.Equal(t, api.PasswordDayChoices, PasswordDayChoices)
}

func TestSaveRefusesUnknownPasswordDays(t *testing.T) {
	f, _ := newFile(t)
	f.PasswordDays = 3
	require.Error(t, f.Save(t.TempDir()), "only Load forgives a choice it doesn't know, nothing writes one")
}
