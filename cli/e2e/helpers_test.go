//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/fs"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/webdav"
)

var dokkyBin string

func TestMain(m *testing.M) {
	fs.GetConfig(context.Background()).LogLevel = fs.LogLevelError
	dir, err := os.MkdirTemp("", "dokky-e2e-bin")
	if err != nil {
		panic(err)
	}
	dokkyBin = filepath.Join(dir, "dokky")
	args := []string{"build", "-o", dokkyBin}
	if runtime.GOOS == "windows" {
		dokkyBin += ".exe"
		args = []string{"build", "-tags", "cmount", "-o", dokkyBin}
	}
	cmd := exec.Command("go", append(args, "../cmd/dokky")...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		panic(fmt.Sprintf("build dokky: %v\n%s", err, out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func startWebDAV(t *testing.T) (url, dir string) {
	t.Helper()
	dir = t.TempDir()
	srv := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(dir), LockSystem: webdav.NewMemLS()})
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv.URL, dir
}

func sparseFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(size))
	require.NoError(t, f.Close())
}

func startEngine(t *testing.T) *engine.Engine {
	t.Helper()
	e, err := engine.Start(t.TempDir(), nil)
	require.NoError(t, err)
	t.Cleanup(e.Stop)
	return e
}

func mountWebDAV(t *testing.T, e *engine.Engine, url, name string, v store.VFS) string {
	t.Helper()
	svc := store.Service{ID: "svc-" + name, Name: name, Type: "webdav", Options: map[string]string{"url": url, "vendor": "other"}}
	require.NoError(t, e.PutService(svc))
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	point := engine.DefaultMountPoint(home, name, runtime.GOOS)
	require.NoError(t, engine.PrepareMountPoint(point))
	actual, err := e.Mount(store.Mount{ID: name, Name: name, ServiceID: svc.ID, VFS: v}, point)
	require.NoError(t, err)
	// a hung call would trip go test's timeout, which panics without cleanups and strands the mount
	if d, ok := t.Deadline(); ok {
		w := time.AfterFunc(time.Until(d)-time.Minute, func() { forceUnmount(actual) })
		t.Cleanup(func() { w.Stop() })
	}
	t.Cleanup(func() {
		if err := e.Unmount(name); err != nil {
			t.Errorf("unmount %s: %v", name, err)
		}
		if isMounted(actual) {
			t.Errorf("%s still mounted after unmount, forcing it off", actual)
			forceUnmount(actual)
		}
		engine.CleanupMountPoint(point)
	})
	if runtime.GOOS == "windows" {
		return actual + `\`
	}
	return actual
}

func isMounted(point string) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	out, err := exec.Command("mount").Output()
	return err == nil && strings.Contains(string(out), " on "+point+" ")
}

func forceUnmount(point string) {
	run := func(name string, args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, name, args...).Run()
	}
	switch runtime.GOOS {
	case "darwin":
		if run("diskutil", "unmount", "force", point) != nil {
			_ = run("umount", "-f", point)
		}
	case "linux":
		_ = run("fusermount3", "-uz", point)
	}
}

// within runs fns concurrently; if they outlive d it force-unmounts point so no hung mount survives the test
func within(t *testing.T, d time.Duration, point string, fns ...func() error) {
	t.Helper()
	errs := make(chan error, len(fns))
	for _, fn := range fns {
		go func() { errs <- fn() }()
	}
	timeout := time.After(d)
	for range fns {
		select {
		case err := <-errs:
			require.NoError(t, err)
		case <-timeout:
			forceUnmount(point)
			t.Fatalf("%s did not answer within %s", point, d)
		}
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	require.Eventually(t, cond, 2*time.Minute, 200*time.Millisecond)
}

func fileIs(path string, want []byte) bool {
	got, err := os.ReadFile(path)
	return err == nil && bytes.Equal(got, want)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	out := make([]string, 0, len(entries))
	for _, en := range entries {
		out = append(out, en.Name())
	}
	return out
}

func randomFile(t *testing.T, path string, size int64) string {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	h := md5.New()
	_, err = io.CopyN(io.MultiWriter(f, h), rand.NewChaCha8([32]byte{1}), size)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return hex.EncodeToString(h.Sum(nil))
}

func md5File(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	h := md5.New()
	_, err = io.Copy(h, f)
	require.NoError(t, err)
	return hex.EncodeToString(h.Sum(nil))
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	require.NoError(t, err)
	defer in.Close()
	out, err := os.Create(dst)
	require.NoError(t, err)
	_, err = io.CopyBuffer(out, in, make([]byte, 1<<20))
	require.NoError(t, err)
	require.NoError(t, out.Close())
}

func readMB(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.ReadFull(f, make([]byte, 1<<20))
	return err
}
