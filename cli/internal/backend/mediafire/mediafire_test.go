package mediafire_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/backend/mediafire"
	"github.com/rclone/rclone/fstest"
	"github.com/rclone/rclone/fstest/fstests"
	"github.com/stretchr/testify/require"
)

func TestIntegration(t *testing.T) {
	if *fstest.RemoteName == "" {
		t.Skip("needs -remote TestMediafire: and a MediaFire test account")
	}
	// fstests looks for rclone's test server scripts under the working directory
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "fstest", "testserver", "init.d"), 0o755))
	t.Chdir(dir)
	fstests.Run(t, &fstests.Opt{
		RemoteName: "TestMediafire:",
		NilObject:  (*mediafire.Object)(nil),
	})
}
