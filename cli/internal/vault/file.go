package vault

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/itzzritik/dokky/cli/internal/platform"
)

const (
	FileName    = "vault.json"
	FileVersion = 1
	maxWrap     = 512 // the server refuses bigger wraps too
)

var PasswordDayChoices = []int{7, 15, 30}

const (
	defaultPasswordDays = 7
	// the password is asked for at least this often, however often system auth slides the window
	passwordCap = 90 * 24 * time.Hour
	// a time saved while the clock ran ahead would keep the window open until then
	clockSkew = time.Hour
	// system auth slides the window by the hour, so vault.json is not written on every unlock
	systemStampEvery = time.Hour
)

// File is vault.json: the account key wrapped three ways plus the password reminder. an older dokky also wrote
// unlock and idleLockMinutes, which are ignored now: the device key always opens the vault at startup
type File struct {
	Version            int       `json:"version"`
	KeyID              string    `json:"keyId"`
	KDF                KDF       `json:"kdf"`
	WrappedKey         string    `json:"wrappedKey"`
	RecoveryWrappedKey string    `json:"recoveryWrappedKey"`
	DeviceWrappedKey   string    `json:"deviceWrappedKey,omitempty"` // empty when this device has no saved key
	PasswordDays       int       `json:"passwordDays"`
	PasswordAt         time.Time `json:"passwordAt"`
	SystemAt           time.Time `json:"systemAt,omitzero"`      // the last system auth, saved at most once an hour
	PasswordOnce       bool      `json:"passwordOnce,omitempty"` // the password changed, so the next unlock types it
	CreatedAt          time.Time `json:"createdAt"`
	Account            bool      `json:"account"` // these wraps match the signed-in account's server vault
}

// Load returns an error wrapping os.ErrNotExist when there is no vault yet
func Load(stateDir string) (*File, error) {
	raw, err := os.ReadFile(filepath.Join(stateDir, FileName))
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s is damaged: %w", FileName, err)
	}
	f.upgrade()
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return &f, nil
}

func (f *File) Save(stateDir string) error {
	if err := f.Validate(); err != nil {
		return fmt.Errorf("refusing to save %s: %w", FileName, err)
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	return platform.WriteFileAtomic(filepath.Join(stateDir, FileName), b)
}

func (f *File) Validate() error {
	if f.Version > FileVersion {
		return fmt.Errorf("it was written by a newer dokky (version %d); update dokky", f.Version)
	}
	if f.Version != FileVersion {
		return fmt.Errorf("version %d is not one this dokky knows", f.Version)
	}
	if err := f.Remote().Validate(); err != nil {
		return err
	}
	if f.DeviceWrappedKey != "" {
		if err := checkWrap(f.DeviceWrappedKey); err != nil {
			return fmt.Errorf("device key wrap: %w", err)
		}
	}
	if !slices.Contains(PasswordDayChoices, f.PasswordDays) {
		return fmt.Errorf("passwordDays %d should be 7, 15 or 30", f.PasswordDays)
	}
	return nil
}

// an older dokky offered 14 days and never, which was 0; a choice this dokky doesn't know, from a newer or a dev
// build, falls back to 15 days instead of stopping the service
func (f *File) upgrade() {
	switch {
	case f.PasswordDays == 14:
		f.PasswordDays = 15
	case f.PasswordDays == 0:
		f.PasswordDays = 30
	case !slices.Contains(PasswordDayChoices, f.PasswordDays):
		f.PasswordDays = 15
	}
}

// PasswordDue says system auth is no longer accepted until the password is typed once: the password changed,
// the interval passed since the later of the typed password and the last system auth, or 90 days passed since
// the typed password. a time from the future counts as due
func (f *File) PasswordDue(now time.Time) bool {
	ahead := now.Add(clockSkew)
	if f.PasswordOnce || f.PasswordAt.After(ahead) || f.SystemAt.After(ahead) {
		return true
	}
	interval := time.Duration(f.PasswordDays) * 24 * time.Hour
	last := f.PasswordAt
	if f.SystemAt.After(last) {
		last = f.SystemAt
	}
	return now.Sub(f.PasswordAt) >= max(passwordCap, interval) || now.Sub(last) >= interval
}

// Typed records the master password typed just now, which restarts the window
func (f *File) Typed(now time.Time) {
	f.PasswordAt, f.PasswordOnce = now.UTC(), false
}

// StampSystem slides the window after a system auth; false when the last stamp is under an hour old, so there is
// nothing to save
func (f *File) StampSystem(now time.Time) bool {
	if d := now.Sub(f.SystemAt); !f.SystemAt.IsZero() && d >= 0 && d < systemStampEvery {
		return false
	}
	f.SystemAt = now.UTC()
	return true
}

// Clone lets a caller change a copy and swap it in only once it is saved
func (f *File) Clone() *File {
	c := *f
	c.KDF.Salt = slices.Clone(f.KDF.Salt)
	return &c
}

// Remote is the part of the vault the server keeps for the account
type Remote struct {
	KeyID              string `json:"keyId"`
	KDF                KDF    `json:"kdf"`
	WrappedKey         string `json:"wrappedKey"`
	RecoveryWrappedKey string `json:"recoveryWrappedKey"`
}

func (f *File) Remote() Remote {
	return Remote{KeyID: f.KeyID, KDF: f.KDF, WrappedKey: f.WrappedKey, RecoveryWrappedKey: f.RecoveryWrappedKey}
}

func (r Remote) Validate() error {
	if !validKeyID(r.KeyID) {
		return fmt.Errorf("key id %q should be 16 lowercase hex characters", r.KeyID)
	}
	if err := r.KDF.Validate(); err != nil {
		return err
	}
	if err := checkWrap(r.WrappedKey); err != nil {
		return fmt.Errorf("password wrap: %w", err)
	}
	if err := checkWrap(r.RecoveryWrappedKey); err != nil {
		return fmt.Errorf("recovery key wrap: %w", err)
	}
	return nil
}

func checkWrap(env string) error {
	if env == "" {
		return errors.New("it is missing")
	}
	b, err := base64.StdEncoding.DecodeString(env)
	if err != nil {
		return errBase64
	}
	if len(b) > maxWrap {
		return fmt.Errorf("it is too long (%d bytes)", len(b))
	}
	return CheckEnvelope(b)
}
