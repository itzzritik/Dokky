//go:build e2e

package e2e

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/librclone/librclone"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadWriteRenameDelete(t *testing.T) {
	url, dir := startWebDAV(t)
	e := startEngine(t)
	mp := mountWebDAV(t, e, url, "rw", store.DefaultVFS())

	data := make([]byte, 10<<20)
	_, _ = rand.NewChaCha8([32]byte{2}).Read(data)
	require.NoError(t, os.WriteFile(filepath.Join(mp, "a.bin"), data, 0o644))
	require.True(t, fileIs(filepath.Join(mp, "a.bin"), data))
	waitFor(t, func() bool { return fileIs(filepath.Join(dir, "a.bin"), data) })

	require.NoError(t, os.Rename(filepath.Join(mp, "a.bin"), filepath.Join(mp, "b.bin")))
	waitFor(t, func() bool { return !exists(filepath.Join(dir, "a.bin")) && fileIs(filepath.Join(dir, "b.bin"), data) })
	require.True(t, fileIs(filepath.Join(mp, "b.bin"), data))

	require.NoError(t, os.Remove(filepath.Join(mp, "b.bin")))
	waitFor(t, func() bool { return !exists(filepath.Join(dir, "b.bin")) })
	require.Empty(t, names(t, mp))
}

func TestLargeFileRoundTrip(t *testing.T) {
	url, dir := startWebDAV(t)
	e := startEngine(t)
	mp := mountWebDAV(t, e, url, "big", store.DefaultVFS())

	src := filepath.Join(t.TempDir(), "src.bin")
	sum := randomFile(t, src, 1<<30)
	copyFile(t, src, filepath.Join(mp, "big.bin"))
	waitFor(t, func() bool {
		q, err := e.Queue("big")
		st, serr := os.Stat(filepath.Join(dir, "big.bin"))
		return err == nil && len(q) == 0 && serr == nil && st.Size() == 1<<30
	})
	require.Equal(t, sum, md5File(t, filepath.Join(dir, "big.bin")))
}

func TestUnicodeAndLongNames(t *testing.T) {
	url, dir := startWebDAV(t)
	e := startEngine(t)
	mp := mountWebDAV(t, e, url, "names", store.DefaultVFS())

	want := []string{"Ünïcode file (1).txt", strings.Repeat("long ünïcode name ", 11) + "end.txt"}
	require.GreaterOrEqual(t, len([]rune(want[1])), 200)
	for _, name := range want {
		require.NoError(t, os.WriteFile(filepath.Join(mp, name), []byte(name), 0o644))
	}
	for _, name := range want {
		waitFor(t, func() bool { return fileIs(filepath.Join(dir, name), []byte(name)) })
		require.True(t, fileIs(filepath.Join(mp, name), []byte(name)), name)
	}
	require.ElementsMatch(t, want, names(t, dir))
	require.ElementsMatch(t, want, names(t, mp))
	require.NoFileExists(t, filepath.Join(dir, ".VolumeIcon.icns"), "the mount icon never reaches the remote")
}

func TestNoFreezeOnSlowBackend(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the freeze is in the macOS NFS client")
	}
	url, dir := startWebDAV(t)
	sparseFile(t, filepath.Join(dir, "movie.bin"), 2<<30)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "other"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other", "note.txt"), []byte("hi"), 0o644))
	e := startEngine(t)
	// options/set BwLimit never reaches the token bucket librclone.Initialize already started
	setBwLimit := func(rate string) {
		out, status := librclone.RPC("core/bwlimit", fmt.Sprintf(`{"rate":%q}`, rate))
		require.Equal(t, http.StatusOK, status, out)
	}
	setBwLimit("10M")
	t.Cleanup(func() { setBwLimit("off") })
	mp := mountWebDAV(t, e, url, "slow", store.DefaultVFS())
	movie := filepath.Join(mp, "movie.bin")

	within(t, 15*time.Second, mp, func() error { return readMB(movie) })
	time.Sleep(6 * time.Second)
	var reopen, list time.Duration
	start := time.Now()
	within(t, 15*time.Second, mp,
		func() error {
			err := readMB(movie)
			reopen = time.Since(start)
			return err
		},
		func() error {
			_, err := os.ReadDir(filepath.Join(mp, "other"))
			list = time.Since(start)
			return err
		},
	)
	t.Logf("reopen read %s, list of another folder %s", reopen.Round(100*time.Millisecond), list.Round(100*time.Millisecond))
}

func TestTwoMountsEjectOne(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Finder eject is macOS only")
	}
	urlA, _ := startWebDAV(t)
	urlB, dirB := startWebDAV(t)
	require.NoError(t, os.Mkdir(filepath.Join(dirB, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dirB, "docs", "b.txt"), []byte("still here"), 0o644))
	e := startEngine(t)
	a := mountWebDAV(t, e, urlA, "a", store.DefaultVFS())
	b := mountWebDAV(t, e, urlB, "b", store.DefaultVFS())

	out, err := exec.Command("diskutil", "unmount", a).CombinedOutput()
	require.NoError(t, err, string(out))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		stats, err := e.Stats()
		assert.NoError(c, err)
		var got []string
		for _, m := range stats {
			got = append(got, m.Name)
		}
		assert.Equal(c, []string{"b"}, got)
	}, 10*time.Second, 200*time.Millisecond)

	within(t, 15*time.Second, b, func() error {
		if _, err := os.ReadDir(filepath.Join(b, "docs")); err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(b, "docs", "b.txt"))
		if err == nil && string(got) != "still here" {
			err = fmt.Errorf("read %q", got)
		}
		return err
	})
	_, err = e.Stats()
	require.NoError(t, err)
}
