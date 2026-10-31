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

func TestProviderOptionsCarryHide(t *testing.T) {
	hide := map[string]bool{}
	for _, o := range provider(t, "sftp").Options {
		hide[o.Name] = o.Hide
	}
	require.True(t, hide["pin_host_key"])
	require.False(t, hide["host"])
}

func TestProviderOptionsCarryTypeAndChoices(t *testing.T) {
	sftp := map[string]Option{}
	for _, o := range provider(t, "sftp").Options {
		sftp[o.Name] = o
	}
	require.Equal(t, "bool", sftp["disable_hashcheck"].Type)
	require.Equal(t, "string", sftp["host"].Type)
	require.False(t, sftp["host"].Exclusive)

	for _, o := range provider(t, "azureblob").Options {
		if o.Name == "delete_snapshots" {
			require.True(t, o.Exclusive)
			require.Equal(t, Example{Value: "include", Help: "Specify 'include' to remove the root blob and all its snapshots"}, o.Examples[1])
			return
		}
	}
	t.Fatal("azureblob has no delete_snapshots option")
}
