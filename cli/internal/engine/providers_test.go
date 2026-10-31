package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func provider(t *testing.T, typ string) Provider {
	t.Helper()
	ps, err := Providers()
	require.NoError(t, err)
	for _, p := range ps {
		if p.Type == typ {
			return p
		}
	}
	t.Fatalf("provider %q not found", typ)
	return Provider{}
}

func TestSplitSecretsSFTP(t *testing.T) {
	plain, secret := SplitSecrets(provider(t, "sftp"), map[string]string{
		"host": "nas.local", "user": "ritik", "port": "22", "pass": "hunter2", "key_pem": "-----BEGIN KEY-----",
	})
	require.Equal(t, map[string]string{"port": "22"}, plain)
	require.Equal(t, map[string]string{"host": "nas.local", "user": "ritik", "pass": "hunter2", "key_pem": "-----BEGIN KEY-----"}, secret)
}

func TestSplitSecretsDriveToken(t *testing.T) {
	plain, secret := SplitSecrets(provider(t, "drive"), map[string]string{
		"token": `{"access_token":"a"}`, "client_secret": "s3cret", "scope": "drive",
	})
	require.Equal(t, map[string]string{"scope": "drive"}, plain)
	require.Equal(t, map[string]string{"token": `{"access_token":"a"}`, "client_secret": "s3cret"}, secret)
}

func TestFeaturedAllExist(t *testing.T) {
	ps, err := Providers()
	require.NoError(t, err)
	types := map[string]bool{}
	for _, p := range ps {
		types[p.Type] = true
	}
	for _, f := range Featured {
		require.True(t, types[f], f)
	}
}
