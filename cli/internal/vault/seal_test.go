package vault

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, KeySize) }

func TestSealRoundTrip(t *testing.T) {
	for _, plain := range [][]byte{nil, []byte("x"), bytes.Repeat([]byte("secret "), 1000)} {
		env, err := Seal(key(1), plain, AADStore)
		require.NoError(t, err)
		got, err := Open(key(1), env, AADStore)
		require.NoError(t, err)
		require.Equal(t, len(plain), len(got))
		require.True(t, bytes.Equal(plain, got))
	}
}

func TestSealShape(t *testing.T) {
	env, err := Seal(key(1), []byte("hello"), AADStore)
	require.NoError(t, err)
	raw, err := base64.StdEncoding.DecodeString(env)
	require.NoError(t, err)
	require.Equal(t, byte(0x01), raw[0])
	require.Len(t, raw, 1+24+5+16)
	require.Equal(t, 41, MinEnvelope)
	require.NotContains(t, string(raw), "hello")
}

func TestSealUsesFreshNonces(t *testing.T) {
	a, err := Seal(key(1), []byte("same"), AADStore)
	require.NoError(t, err)
	b, err := Seal(key(1), []byte("same"), AADStore)
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

func TestOpenRejectsWrongKeyAndRole(t *testing.T) {
	env, err := Seal(key(1), []byte("token"), AADServiceToken("s1"))
	require.NoError(t, err)

	_, err = Open(key(2), env, AADServiceToken("s1"))
	require.ErrorIs(t, err, ErrOpen)
	_, err = Open(key(1), env, AADServiceToken("s2"))
	require.ErrorIs(t, err, ErrOpen, "a blob moved to another service must not open")
	_, err = Open(key(1), env, AADServiceConfig("s1"))
	require.ErrorIs(t, err, ErrOpen, "a token must not open as a config")
	_, err = Open(key(1), env, AADStore)
	require.ErrorIs(t, err, ErrOpen)
}

func TestOpenRejectsTampering(t *testing.T) {
	raw, err := SealBytes(key(1), []byte("drive login"), AADStore)
	require.NoError(t, err)
	for i := 1; i < len(raw); i++ {
		bad := bytes.Clone(raw)
		bad[i] ^= 0x01
		_, err := OpenBytes(key(1), bad, AADStore)
		require.ErrorIs(t, err, ErrOpen, "byte %d", i)
	}
	_, err = OpenBytes(key(1), raw[:len(raw)-1], AADStore)
	require.ErrorIs(t, err, ErrOpen)
	_, err = OpenBytes(key(1), append(bytes.Clone(raw), 0), AADStore)
	require.ErrorIs(t, err, ErrOpen)
}

func TestOpenRejectsBadShapes(t *testing.T) {
	raw, err := SealBytes(key(1), []byte("x"), AADStore)
	require.NoError(t, err)

	v2 := bytes.Clone(raw)
	v2[0] = 0x02
	_, err = OpenBytes(key(1), v2, AADStore)
	require.ErrorContains(t, err, "update dokky")
	require.NotErrorIs(t, err, ErrOpen)

	_, err = OpenBytes(key(1), raw[:MinEnvelope-1], AADStore)
	require.ErrorContains(t, err, "too short")
	_, err = OpenBytes(key(1), nil, AADStore)
	require.ErrorContains(t, err, "too short")

	_, err = Open(key(1), "not base64!", AADStore)
	require.ErrorContains(t, err, "base64")

	_, err = Seal(make([]byte, 16), []byte("x"), AADStore)
	require.ErrorContains(t, err, "16 bytes")
	_, err = OpenBytes(make([]byte, 31), raw, AADStore)
	require.ErrorContains(t, err, "31 bytes")

	require.NoError(t, CheckEnvelope(raw))
	require.Error(t, CheckEnvelope(v2))
}

func TestAADStrings(t *testing.T) {
	require.Equal(t, "dokky/v1/wrap|password|3f2a9c0011223344", AADWrap(WrapPassword, "3f2a9c0011223344"))
	require.Equal(t, "dokky/v1/wrap|recovery|3f2a9c0011223344", AADWrap(WrapRecovery, "3f2a9c0011223344"))
	require.Equal(t, "dokky/v1/wrap|device|3f2a9c0011223344", AADWrap(WrapDevice, "3f2a9c0011223344"))
	require.Equal(t, "dokky/v1/service|s1|config", AADServiceConfig("s1"))
	require.Equal(t, "dokky/v1/service|s1|token", AADServiceToken("s1"))
	require.Equal(t, "dokky/v1/store", AADStore)
}
