package vault

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

var (
	ErrWrongPassword = errors.New("wrong master password")
	ErrNoDeviceKey   = errors.New("this device has no saved key for the vault")
	ErrDeviceKey     = errors.New("the key saved on this device does not open the vault")
	errOtherKey      = errors.New("this account key does not belong to the vault")
)

// Create makes a new account key and wraps it with the password, a new recovery key and the device key.
// without a device key the vault opens with the password only
func Create(password, deviceKey []byte, now time.Time) (f *File, ak []byte, recoveryText string, err error) {
	ak = NewAccountKey()
	f = &File{
		Version:         FileVersion,
		KeyID:           KeyID(ak),
		KDF:             NewKDF(),
		Unlock:          UnlockAuto,
		PasswordDays:    defaultPasswordDays,
		IdleLockMinutes: defaultIdleLock,
		PasswordAt:      now.UTC(),
		CreatedAt:       now.UTC(),
	}
	if err := f.setPassword(ak, password); err != nil {
		Zero(ak)
		return nil, nil, "", err
	}
	if recoveryText, err = NewRecovery(f, ak); err != nil {
		Zero(ak)
		return nil, nil, "", err
	}
	if len(deviceKey) == 0 {
		f.Unlock = UnlockPassword
	} else if err := f.SetDevice(ak, deviceKey); err != nil {
		Zero(ak)
		return nil, nil, "", err
	}
	return f, ak, recoveryText, nil
}

func OpenWithPassword(f *File, password []byte) ([]byte, error) {
	return OpenRemoteWithPassword(f.Remote(), password)
}

// OpenRemoteWithPassword opens the account key from the server's vault, for joining an account
func OpenRemoteWithPassword(r Remote, password []byte) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if len(password) == 0 {
		return nil, ErrWrongPassword
	}
	kek, err := PasswordKEK(password, r.KDF)
	if err != nil {
		return nil, err
	}
	defer Zero(kek)
	ak, err := Unwrap(kek, r.WrappedKey, WrapPassword, r.KeyID)
	if errors.Is(err, ErrOpen) {
		return nil, ErrWrongPassword
	}
	return ak, err
}

func OpenWithDevice(f *File, deviceKey []byte) ([]byte, error) {
	if f.DeviceWrappedKey == "" || len(deviceKey) == 0 {
		return nil, ErrNoDeviceKey
	}
	kek, err := DeviceKEK(deviceKey)
	if err != nil {
		return nil, err
	}
	defer Zero(kek)
	ak, err := Unwrap(kek, f.DeviceWrappedKey, WrapDevice, f.KeyID)
	if errors.Is(err, ErrOpen) {
		return nil, ErrDeviceKey
	}
	return ak, err
}

func OpenWithRecovery(f *File, recoveryText string) ([]byte, error) {
	return OpenRemoteWithRecovery(f.Remote(), recoveryText)
}

func OpenRemoteWithRecovery(r Remote, recoveryText string) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	raw, err := ParseRecoveryKey(recoveryText)
	if err != nil {
		return nil, err
	}
	defer Zero(raw)
	kek, err := RecoveryKEK(raw, r.KDF.Salt)
	if err != nil {
		return nil, err
	}
	defer Zero(kek)
	ak, err := Unwrap(kek, r.RecoveryWrappedKey, WrapRecovery, r.KeyID)
	if errors.Is(err, ErrOpen) {
		return nil, fmt.Errorf("%w: it does not open this vault", ErrBadRecoveryKey)
	}
	return ak, err
}

// Rewrap changes the master password. the salt stays because the recovery wrap is derived from it,
// so the recovery key keeps working; the cost only ever goes up to today's defaults
func Rewrap(f *File, ak, newPassword []byte, now time.Time) error {
	if KeyID(ak) != f.KeyID {
		return errOtherKey
	}
	if len(newPassword) == 0 {
		return errors.New("the new password is empty")
	}
	next := f.Clone()
	next.KDF.T = max(next.KDF.T, cost.t)
	next.KDF.M = max(next.KDF.M, cost.m)
	next.KDF.P = max(next.KDF.P, cost.p)
	if err := next.setPassword(ak, newPassword); err != nil {
		return err
	}
	f.KDF, f.WrappedKey, f.PasswordAt = next.KDF, next.WrappedKey, now.UTC()
	return nil
}

// NewRecovery replaces the recovery wrap and returns the new key's text, shown once
func NewRecovery(f *File, ak []byte) (string, error) {
	if KeyID(ak) != f.KeyID {
		return "", errOtherKey
	}
	raw, text := NewRecoveryKey()
	defer Zero(raw)
	kek, err := RecoveryKEK(raw, f.KDF.Salt)
	if err != nil {
		return "", err
	}
	defer Zero(kek)
	env, err := Wrap(kek, ak, WrapRecovery, f.KeyID)
	if err != nil {
		return "", err
	}
	f.RecoveryWrappedKey = env
	return text, nil
}

// SetDevice wraps the account key with this device's key, so it can open without a prompt
func (f *File) SetDevice(ak, deviceKey []byte) error {
	if KeyID(ak) != f.KeyID {
		return errOtherKey
	}
	kek, err := DeviceKEK(deviceKey)
	if err != nil {
		return err
	}
	defer Zero(kek)
	env, err := Wrap(kek, ak, WrapDevice, f.KeyID)
	if err != nil {
		return err
	}
	f.DeviceWrappedKey = env
	return nil
}

func (f *File) setPassword(ak, password []byte) error {
	kek, err := PasswordKEK(password, f.KDF)
	if err != nil {
		return err
	}
	defer Zero(kek)
	env, err := Wrap(kek, ak, WrapPassword, f.KeyID)
	if err != nil {
		return err
	}
	f.WrappedKey = env
	return nil
}

// AdoptRemote returns local with the server's key, wraps and password settings in place of its own and a fresh
// device wrap, for a device joining an account. local is not changed and may be nil for a device with no vault yet.
// a device set to unlock with the password gets no device wrap
func AdoptRemote(local *File, remote Remote, ak, deviceKey []byte, now time.Time) (*File, error) {
	if err := remote.Validate(); err != nil {
		return nil, fmt.Errorf("the account's vault: %w", err)
	}
	if KeyID(ak) != remote.KeyID {
		return nil, errOtherKey
	}
	f := &File{
		Version:         FileVersion,
		Unlock:          UnlockAuto,
		PasswordDays:    defaultPasswordDays,
		IdleLockMinutes: defaultIdleLock,
		CreatedAt:       now.UTC(),
	}
	if local != nil {
		f = local.Clone()
	}
	f.KeyID, f.KDF, f.WrappedKey, f.RecoveryWrappedKey = remote.KeyID, remote.KDF, remote.WrappedKey, remote.RecoveryWrappedKey
	f.KDF.Salt = slices.Clone(remote.KDF.Salt)
	f.DeviceWrappedKey = ""
	f.PasswordAt = now.UTC()
	f.Account = true
	switch {
	case f.Unlock == UnlockPassword:
	case len(deviceKey) == 0:
		f.Unlock = UnlockPassword
	default:
		if err := f.SetDevice(ak, deviceKey); err != nil {
			return nil, err
		}
	}
	return f, nil
}
