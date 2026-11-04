package engine

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/stretchr/testify/require"
)

func sftpService() store.Service {
	return store.Service{
		ID: "svc1", Name: "nas", Type: "sftp",
		Options: map[string]string{"host": "nas.local"},
		Secrets: map[string]string{"pass": "hunter2"},
	}
}

func isPass(typ, key string) bool { return typ == "sftp" && key == "pass" }

func TestPutServiceObscuresPasswordsOnly(t *testing.T) {
	mc := newMemConfig(nil)
	mc.PutService(sftpService(), isPass)

	host, ok := mc.GetValue("svc1", "host")
	require.True(t, ok)
	require.Equal(t, "nas.local", host)
	typ, _ := mc.GetValue("svc1", "type")
	require.Equal(t, "sftp", typ)

	pass, ok := mc.GetValue("svc1", "pass")
	require.True(t, ok)
	require.NotEqual(t, "hunter2", pass)
	require.Equal(t, "hunter2", obscure.MustReveal(pass))

	s := sftpService()
	s.Secrets = nil
	mc.PutService(s, isPass)
	_, ok = mc.GetValue("svc1", "pass")
	require.False(t, ok)

	mc.DeleteService("svc1")
	require.False(t, mc.HasSection("svc1"))
}

func TestTokenSetFiresCallbackOnce(t *testing.T) {
	var calls [][2]string
	mc := newMemConfig(func(id, tok string) { calls = append(calls, [2]string{id, tok}) })
	mc.SetValue("svc1", "token", "abc")
	mc.SetValue("svc1", "token", "abc")
	mc.SetValue("svc1", "other", "x")
	require.Equal(t, [][2]string{{"svc1", "abc"}}, calls)
	mc.SetValue("svc1", "token", "def")
	require.Len(t, calls, 2)
}

func TestNoConfigFileWritten(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfgPath := filepath.Join(home, "rclone.conf")
	t.Setenv("RCLONE_CONFIG", cfgPath)

	oldData, oldPath := config.Data(), config.GetConfigPath()
	t.Cleanup(func() {
		require.NoError(t, config.SetConfigPath(oldPath))
		config.SetData(oldData)
	})
	require.NoError(t, config.SetConfigPath(cfgPath))

	mc := newMemConfig(nil)
	mc.PutService(sftpService(), isPass)
	config.SetData(mc)
	config.SaveConfig()

	require.Same(t, mc, config.Data())
	var files []string
	require.NoError(t, filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return err
	}))
	require.Empty(t, files)
}

func TestPutServiceAddsS3Defaults(t *testing.T) {
	mc := newMemConfig(nil)
	mc.PutService(store.Service{ID: "s3", Type: "s3", Options: map[string]string{"provider": "Other", "chunk_size": "16M"}}, isPass)

	for k, want := range map[string]string{"upload_cutoff": "8M", "upload_concurrency": "32", "chunk_size": "16M", "directory_markers": "true", "use_server_modtime": "true"} {
		got, _ := mc.GetValue("s3", k)
		require.Equal(t, want, got, k)
	}
	_, ok := mc.GetValue("svc1", "upload_cutoff")
	require.False(t, ok)
}
