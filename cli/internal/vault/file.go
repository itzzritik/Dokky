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

// how the daemon opens the vault when it starts
const (
	UnlockAuto     = "auto"     // the device key, no prompt
	UnlockSystem   = "system"   // Touch ID, Windows Hello or polkit, then the device key
	UnlockPassword = "password" // the master password every time
)

var (
	PasswordDayChoices = []int{7, 14, 30, 0}
	IdleLockChoices    = []int{5, 10, 30, 60, 0}
)

const (
	defaultPasswordDays = 14
	defaultIdleLock     = 10
)

// File is vault.json: the account key wrapped three ways plus the unlock settings
type File struct {
	Version            int       `json:"version"`
	KeyID              string    `json:"keyId"`
	KDF                KDF       `json:"kdf"`
	WrappedKey         string    `json:"wrappedKey"`
	RecoveryWrappedKey string    `json:"recoveryWrappedKey"`
	DeviceWrappedKey   string    `json:"deviceWrappedKey,omitempty"` // empty when this device has no saved key
	Unlock             string    `json:"unlock"`
	PasswordDays       int       `json:"passwordDays"`
	IdleLockMinutes    int       `json:"idleLockMinutes"`
	PasswordAt         time.Time `json:"passwordAt"`
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
	if !ValidUnlock(f.Unlock) {
		return fmt.Errorf("unlock %q should be auto, system or password", f.Unlock)
	}
	if !slices.Contains(PasswordDayChoices, f.PasswordDays) {
		return fmt.Errorf("passwordDays %d should be 0, 7, 14 or 30", f.PasswordDays)
	}
	if !slices.Contains(IdleLockChoices, f.IdleLockMinutes) {
		return fmt.Errorf("idleLockMinutes %d should be 0, 5, 10, 30 or 60", f.IdleLockMinutes)
	}
	return nil
}

func ValidUnlock(s string) bool {
	return s == UnlockAuto || s == UnlockSystem || s == UnlockPassword
}

// PasswordDue says system auth is no longer accepted until the password is typed once
func (f *File) PasswordDue(now time.Time) bool {
	return f.PasswordDays > 0 && now.Sub(f.PasswordAt) >= time.Duration(f.PasswordDays)*24*time.Hour
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
