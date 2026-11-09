package vault

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeyID(t *testing.T) {
	require.Equal(t, "c36f75f54394483a", KeyID(make([]byte, 32)))
	ak := NewAccountKey()
	require.Len(t, ak, 32)
	require.True(t, validKeyID(KeyID(ak)))
	require.Equal(t, KeyID(ak), KeyID(ak))
	require.NotEqual(t, KeyID(ak), KeyID(NewAccountKey()))

	for _, bad := range []string{"", "c36f75f54394483", "c36f75f54394483ab", "C36F75F54394483A", "c36f75f54394483g"} {
		require.False(t, validKeyID(bad), bad)
	}
}

func TestWrapUnwrap(t *testing.T) {
	ak := NewAccountKey()
	id := KeyID(ak)
	env, err := Wrap(key(9), ak, WrapPassword, id)
	require.NoError(t, err)

	got, err := Unwrap(key(9), env, WrapPassword, id)
	require.NoError(t, err)
	require.Equal(t, ak, got)

	_, err = Unwrap(key(8), env, WrapPassword, id)
	require.ErrorIs(t, err, ErrOpen)
	_, err = Unwrap(key(9), env, WrapRecovery, id)
	require.ErrorIs(t, err, ErrOpen, "a password wrap must not open as a recovery wrap")
	_, err = Unwrap(key(9), env, WrapPassword, KeyID(NewAccountKey()))
	require.ErrorIs(t, err, ErrOpen)

	_, err = Wrap(key(9), ak[:16], WrapPassword, id)
	require.Error(t, err)
}

// a server that wraps some other key under this account's key id is caught
func TestUnwrapChecksKeyID(t *testing.T) {
	ak, other := NewAccountKey(), NewAccountKey()
	env, err := Wrap(key(9), other, WrapDevice, KeyID(ak))
	require.NoError(t, err)
	_, err = Unwrap(key(9), env, WrapDevice, KeyID(ak))
	require.ErrorIs(t, err, ErrOpen)
}

func TestZero(t *testing.T) {
	b := []byte{1, 2, 3}
	Zero(b)
	require.Equal(t, []byte{0, 0, 0}, b)
	Zero(nil)
}
