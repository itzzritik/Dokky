package vault

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceRoundTrip(t *testing.T) {
	ak := NewAccountKey()
	options := map[string]string{"host": "nas.local", "user": "ritik"}
	secrets := map[string]string{"pass": "hunter2", "token": `{"access_token":"ya29"}`}

	sealed, sealedToken, err := SealService(ak, "s1", options, secrets)
	require.NoError(t, err)
	require.NotNil(t, sealedToken)
	require.NotContains(t, sealed, "hunter2")
	require.Contains(t, secrets, "token", "the caller's map is left alone")

	gotOpts, gotSecrets, err := OpenService(ak, "s1", sealed, sealedToken)
	require.NoError(t, err)
	require.Equal(t, options, gotOpts)
	require.Equal(t, secrets, gotSecrets)

	plain, err := Open(ak, sealed, AADServiceConfig("s1"))
	require.NoError(t, err)
	var cfg map[string]map[string]string
	require.NoError(t, json.Unmarshal(plain, &cfg))
	require.Equal(t, map[string]string{"pass": "hunter2"}, cfg["secrets"], "the token is not in the config")
	require.Equal(t, options, cfg["options"])
}

func TestServiceWithoutToken(t *testing.T) {
	ak := NewAccountKey()
	sealed, sealedToken, err := SealService(ak, "s1", nil, nil)
	require.NoError(t, err)
	require.Nil(t, sealedToken)
	opts, secrets, err := OpenService(ak, "s1", sealed, nil)
	require.NoError(t, err)
	require.NotNil(t, opts)
	require.NotNil(t, secrets)
	require.Empty(t, opts)
	require.Empty(t, secrets)
}

func TestServiceBlobsAreBoundToTheirService(t *testing.T) {
	ak := NewAccountKey()
	s1, t1, err := SealService(ak, "s1", nil, map[string]string{"token": "one"})
	require.NoError(t, err)
	s2, t2, err := SealService(ak, "s2", nil, map[string]string{"token": "two"})
	require.NoError(t, err)

	_, _, err = OpenService(ak, "s2", s1, t2)
	require.ErrorIs(t, err, ErrOpen, "config moved to another service")
	_, _, err = OpenService(ak, "s2", s2, t1)
	require.ErrorIs(t, err, ErrOpen, "token moved to another service")
	_, _, err = OpenService(ak, "s1", *t1, nil)
	require.ErrorIs(t, err, ErrOpen, "token used as a config")
	_, _, err = OpenService(NewAccountKey(), "s1", s1, t1)
	require.ErrorIs(t, err, ErrOpen)

	raw, err := base64.StdEncoding.DecodeString(s1)
	require.NoError(t, err)
	raw[len(raw)-1] ^= 1
	_, _, err = OpenService(ak, "s1", base64.StdEncoding.EncodeToString(raw), t1)
	require.ErrorIs(t, err, ErrOpen)
}

// a token hidden in the config never beats the sealed token
func TestServiceTokenComesFromItsOwnEnvelope(t *testing.T) {
	ak := NewAccountKey()
	plain := `{"options":{},"secrets":{"token":"planted"}}`
	sealed, err := Seal(ak, []byte(plain), AADServiceConfig("s1"))
	require.NoError(t, err)
	_, secrets, err := OpenService(ak, "s1", sealed, nil)
	require.NoError(t, err)
	require.NotContains(t, secrets, "token")
}

func TestServiceErrors(t *testing.T) {
	ak := NewAccountKey()
	_, _, err := SealService(ak, "", nil, nil)
	require.Error(t, err)
	_, _, err = SealService(ak[:8], "s1", nil, nil)
	require.Error(t, err)

	junk, err := Seal(ak, []byte("not json"), AADServiceConfig("s1"))
	require.NoError(t, err)
	_, _, err = OpenService(ak, "s1", junk, nil)
	require.ErrorContains(t, err, "damaged")
	require.True(t, strings.Contains(err.Error(), "s1"))
}
