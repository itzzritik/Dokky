package engine

import (
	"io"
	"os"
	"testing"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/stretchr/testify/require"
	"github.com/willscott/go-nfs/file"
)

func TestIconFSServesRootIconsReadOnly(t *testing.T) {
	inner := memfs.New()
	f := wrapFS(inner, nil)

	for name, want := range map[string][]byte{"._.": customIconFlag, "/.VolumeIcon.icns": volumeIcon} {
		info, err := f.Lstat(name)
		require.NoError(t, err, name)
		require.Equal(t, int64(len(want)), info.Size())
		require.Equal(t, uint32(os.Getuid()), info.Sys().(*file.FileInfo).UID, "macOS ignores AppleDouble files owned by someone else")
		require.False(t, info.ModTime().IsZero())
		fh, err := f.Open(name)
		require.NoError(t, err)
		got, err := io.ReadAll(fh)
		require.NoError(t, err)
		require.Equal(t, want, got)

		_, err = f.OpenFile(name, os.O_RDWR, 0)
		require.ErrorIs(t, err, os.ErrPermission)
		require.ErrorIs(t, f.Remove(name), os.ErrPermission)
	}
	require.Equal(t, []byte{0x04, 0x00}, customIconFlag[0x3a:0x3c], "kHasCustomIcon")

	for _, root := range []string{"/", "", "."} {
		entries, err := f.ReadDir(root)
		require.NoError(t, err)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		require.ElementsMatch(t, []string{"._.", ".VolumeIcon.icns"}, names, "macOS skips lookups for names a listing didn't show")
	}
	require.NoError(t, inner.MkdirAll("Media", 0o755))
	sub, err := f.ReadDir("Media")
	require.NoError(t, err)
	require.Empty(t, sub)
	_, err = inner.Stat(".VolumeIcon.icns")
	require.ErrorIs(t, err, os.ErrNotExist, "nothing reaches the remote")
}

func TestIconFSRefusesNewJunkButKeepsExisting(t *testing.T) {
	inner := memfs.New()
	f := wrapFS(inner, nil)

	for _, name := range []string{"Media/._movie.mkv", ".DS_Store", "a/b/.DS_Store", "._x"} {
		_, err := f.Create(name)
		require.ErrorIs(t, err, os.ErrPermission, name)
	}
	fh, err := f.Create("Media/movie.mkv")
	require.NoError(t, err)
	require.NoError(t, fh.Close())
	require.ErrorIs(t, f.Rename("Media/movie.mkv", "Media/._movie.mkv"), os.ErrPermission)

	old, err := inner.Create("Media/._old")
	require.NoError(t, err)
	require.NoError(t, old.Close())
	fh, err = f.OpenFile("Media/._old", os.O_RDWR|os.O_CREATE, 0o644)
	require.NoError(t, err, "files already on the remote still open")
	require.NoError(t, fh.Close())
}

func TestIconHandlerUnwrapsForRclone(t *testing.T) {
	inner := memfs.New()
	require.Same(t, unwrapFS(wrapFS(inner, nil)), inner)
	require.Nil(t, wrapFS(nil, nil))
}

func TestReapSignalsEject(t *testing.T) {
	e := &Engine{mounted: map[string]liveMount{}, ejected: make(chan struct{}, 1)}
	ran := false
	e.mounted["nas"] = liveMount{point: "/nowhere/nas", onUnmount: func() { ran = true }}
	e.mounted["docs"] = liveMount{point: "/", onUnmount: func() {}}

	require.True(t, e.reap(map[string]bool{"/": true}))
	require.True(t, ran)
	require.NotContains(t, e.mounted, "nas")
	require.Contains(t, e.mounted, "docs")
	select {
	case <-e.Ejected():
	default:
		t.Fatal("no eject signal")
	}
	require.False(t, e.reap(map[string]bool{"/": true}))
	select {
	case <-e.Ejected():
		t.Fatal("signalled with nothing ejected")
	default:
	}
}

func TestIconHandlerKeepsListingsForPaging(t *testing.T) {
	h := newIconHandler(nil)
	inner := memfs.New()
	_, _ = inner.Create("a")
	first, err := inner.ReadDir("/")
	require.NoError(t, err)

	v := h.VerifierFor("dir", first)
	require.NotZero(t, v)
	require.NoError(t, inner.Remove("a"))
	require.Equal(t, first, h.DataForVerifier("dir", v), "later pages see the listing they started with")
	require.Nil(t, h.DataForVerifier("other", v))
	require.NotEqual(t, v, h.VerifierFor("dir", nil))

	for range maxListings {
		h.VerifierFor("dir", nil)
	}
	require.Nil(t, h.DataForVerifier("dir", v), "old listings are dropped")
}
