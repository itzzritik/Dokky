package vault

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var pw = []byte("correct horse battery")

func TestCreateOpensEveryWay(t *testing.T) {
	f, ak, text, err := Create(pw, key(5), now)
	require.NoError(t, err)
	require.Regexp(t, recoveryShape, text)
	require.Equal(t, KeyID(ak), f.KeyID)
	require.False(t, f.Account)
	require.Equal(t, now, f.PasswordAt)
	require.NoError(t, f.Validate())

	got, err := OpenWithPassword(f, pw)
	require.NoError(t, err)
	require.Equal(t, ak, got)

	got, err = OpenWithDevice(f, key(5))
	require.NoError(t, err)
	require.Equal(t, ak, got)

	got, err = OpenWithRecovery(f, text)
	require.NoError(t, err)
	require.Equal(t, ak, got)
}

func TestOpenFailures(t *testing.T) {
	f, _, _, err := Create(pw, key(5), now)
	require.NoError(t, err)

	_, err = OpenWithPassword(f, []byte("correct horse batterz"))
	require.ErrorIs(t, err, ErrWrongPassword)
	_, err = OpenWithPassword(f, nil)
	require.ErrorIs(t, err, ErrWrongPassword)

	_, err = OpenWithDevice(f, key(6))
	require.ErrorIs(t, err, ErrDeviceKey)
	_, err = OpenWithDevice(f, nil)
	require.ErrorIs(t, err, ErrNoDeviceKey)

	_, other := NewRecoveryKey()
	_, err = OpenWithRecovery(f, other)
	require.ErrorIs(t, err, ErrBadRecoveryKey)
	_, err = OpenWithRecovery(f, "DK1-nonsense")
	require.ErrorIs(t, err, ErrBadRecoveryKey)
}

func TestCreateWithoutDeviceKey(t *testing.T) {
	f, ak, _, err := Create(pw, nil, now)
	require.NoError(t, err)
	require.Empty(t, f.DeviceWrappedKey)
	_, err = OpenWithDevice(f, key(5))
	require.ErrorIs(t, err, ErrNoDeviceKey)

	require.NoError(t, f.SetDevice(ak, key(5)))
	got, err := OpenWithDevice(f, key(5))
	require.NoError(t, err)
	require.Equal(t, ak, got)
}

// a server swapping the password and recovery wraps, or wraps between accounts, gets nothing
func TestSwappedWrapsFail(t *testing.T) {
	f, _, text, err := Create(pw, key(5), now)
	require.NoError(t, err)
	swapped := f.Clone()
	swapped.WrappedKey, swapped.RecoveryWrappedKey = f.RecoveryWrappedKey, f.WrappedKey
	_, err = OpenWithPassword(swapped, pw)
	require.ErrorIs(t, err, ErrWrongPassword)
	_, err = OpenWithRecovery(swapped, text)
	require.ErrorIs(t, err, ErrBadRecoveryKey)

	g, _, _, err := Create(pw, key(5), now)
	require.NoError(t, err)
	mixed := f.Clone()
	mixed.WrappedKey = g.WrappedKey
	_, err = OpenWithPassword(mixed, pw)
	require.Error(t, err)
	mixed = f.Clone()
	mixed.DeviceWrappedKey = g.DeviceWrappedKey
	_, err = OpenWithDevice(mixed, key(5))
	require.ErrorIs(t, err, ErrDeviceKey)
}

func TestRewrap(t *testing.T) {
	f, ak, text, err := Create(pw, key(5), now)
	require.NoError(t, err)
	before := f.Clone()
	later := now.Add(time.Hour)
	next := []byte("a whole new password")

	require.NoError(t, Rewrap(f, ak, next, later))
	require.Equal(t, before.KeyID, f.KeyID)
	require.Equal(t, before.KDF.Salt, f.KDF.Salt)
	require.NotEqual(t, before.WrappedKey, f.WrappedKey)
	require.Equal(t, before.RecoveryWrappedKey, f.RecoveryWrappedKey)
	require.Equal(t, before.DeviceWrappedKey, f.DeviceWrappedKey)
	require.Equal(t, later, f.PasswordAt)

	_, err = OpenWithPassword(f, pw)
	require.ErrorIs(t, err, ErrWrongPassword)
	got, err := OpenWithPassword(f, next)
	require.NoError(t, err)
	require.Equal(t, ak, got)
	got, err = OpenWithRecovery(f, text)
	require.NoError(t, err, "the recovery key survives a password change")
	require.Equal(t, ak, got)
	got, err = OpenWithDevice(f, key(5))
	require.NoError(t, err)
	require.Equal(t, ak, got)

	require.ErrorIs(t, Rewrap(f, NewAccountKey(), next, later), errOtherKey)
	require.Error(t, Rewrap(f, ak, nil, later))
}

func TestRewrapRaisesCost(t *testing.T) {
	f, ak, _, err := Create(pw, key(5), now)
	require.NoError(t, err)
	f.KDF.T = 1
	cost.t = 2
	t.Cleanup(func() { cost.t = 1 })
	require.NoError(t, Rewrap(f, ak, pw, now))
	require.Equal(t, uint32(2), f.KDF.T)
}

func TestNewRecovery(t *testing.T) {
	f, ak, old, err := Create(pw, key(5), now)
	require.NoError(t, err)
	text, err := NewRecovery(f, ak)
	require.NoError(t, err)
	require.NotEqual(t, old, text)

	_, err = OpenWithRecovery(f, old)
	require.ErrorIs(t, err, ErrBadRecoveryKey)
	got, err := OpenWithRecovery(f, text)
	require.NoError(t, err)
	require.Equal(t, ak, got)

	_, err = NewRecovery(f, NewAccountKey())
	require.ErrorIs(t, err, errOtherKey)
}

func TestAdoptRemote(t *testing.T) {
	// device A made the account's vault; device B has its own and joins with A's password
	a, ak, text, err := Create(pw, key(1), now)
	require.NoError(t, err)
	a.Account = true
	remote := a.Remote()

	b, _, _, err := Create([]byte("device b password"), key(2), now)
	require.NoError(t, err)
	b.PasswordDays = 30
	before := b.Clone()

	joined, err := OpenRemoteWithPassword(remote, pw)
	require.NoError(t, err)
	require.Equal(t, ak, joined)

	later := now.Add(time.Hour)
	f, err := AdoptRemote(b, remote, joined, key(2), later)
	require.NoError(t, err)
	require.Equal(t, before, b, "the local file is not changed")
	require.NoError(t, f.Validate())
	require.Equal(t, remote, f.Remote())
	require.True(t, f.Account)
	require.Equal(t, later, f.PasswordAt)
	require.Equal(t, before.CreatedAt, f.CreatedAt)
	require.Equal(t, 30, f.PasswordDays)

	for _, open := range []func() ([]byte, error){
		func() ([]byte, error) { return OpenWithPassword(f, pw) },
		func() ([]byte, error) { return OpenWithDevice(f, key(2)) },
		func() ([]byte, error) { return OpenWithRecovery(f, text) },
	} {
		got, err := open()
		require.NoError(t, err)
		require.Equal(t, ak, got)
	}
	_, err = OpenWithDevice(f, key(1))
	require.ErrorIs(t, err, ErrDeviceKey, "only this device's key is wrapped")
	_, err = OpenWithPassword(f, []byte("device b password"))
	require.ErrorIs(t, err, ErrWrongPassword)

	_, err = AdoptRemote(b, remote, NewAccountKey(), key(2), later)
	require.ErrorIs(t, err, errOtherKey)
}

func TestAdoptRemoteWithoutLocalVault(t *testing.T) {
	a, ak, _, err := Create(pw, key(1), now)
	require.NoError(t, err)
	f, err := AdoptRemote(nil, a.Remote(), ak, key(3), now)
	require.NoError(t, err)
	require.NoError(t, f.Validate())
	got, err := OpenWithDevice(f, key(3))
	require.NoError(t, err)
	require.Equal(t, ak, got)

	f, err = AdoptRemote(nil, a.Remote(), ak, nil, now)
	require.NoError(t, err)
	require.Empty(t, f.DeviceWrappedKey, "no keychain, so it opens with the password only")
}

// a device once set to open with the password only gets a device wrap like any other
func TestAdoptRemoteAlwaysWrapsForTheDevice(t *testing.T) {
	a, ak, _, err := Create(pw, nil, now)
	require.NoError(t, err)
	f, err := AdoptRemote(a.Clone(), a.Remote(), ak, key(3), now)
	require.NoError(t, err)
	got, err := OpenWithDevice(f, key(3))
	require.NoError(t, err)
	require.Equal(t, ak, got)
}

func TestRemoteValidation(t *testing.T) {
	a, ak, _, err := Create(pw, key(1), now)
	require.NoError(t, err)
	hostile := a.Remote()
	hostile.KDF.M = 4 << 20
	start := time.Now()
	_, err = OpenRemoteWithPassword(hostile, pw)
	require.Error(t, err)
	require.Less(t, time.Since(start), time.Second)
	_, err = AdoptRemote(nil, hostile, ak, key(1), now)
	require.Error(t, err)

	bad := a.Remote()
	bad.KeyID = "nope"
	_, err = OpenRemoteWithRecovery(bad, "x")
	require.Error(t, err)
}

func TestRealCostRoundTrip(t *testing.T) {
	useRealCost(t)
	f, ak, _, err := Create(pw, key(5), now)
	require.NoError(t, err)
	require.Equal(t, KDF{Alg: "argon2id", Salt: f.KDF.Salt, T: 3, M: 65536, P: 4}, f.KDF)
	got, err := OpenWithPassword(f, pw)
	require.NoError(t, err)
	require.Equal(t, ak, got)
}
