package engine

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"net"
	"os"
	"os/exec"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	billy "github.com/go-git/go-billy/v5"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/rclone/rclone/cmd/mountlib"
	"github.com/rclone/rclone/cmd/serve/nfs"
	"github.com/rclone/rclone/vfs"
	gonfs "github.com/willscott/go-nfs"
	"github.com/willscott/go-nfs/file"
	"golang.org/x/sys/unix"
)

//go:embed assets/dokky.icns
var volumeIcon []byte

// a macOS-written AppleDouble whose only attribute is FinderInfo with kHasCustomIcon set
//
//go:embed assets/finderinfo-custom-icon
var customIconFlag []byte

var rootIcons = map[string][]byte{"._.": customIconFlag, ".VolumeIcon.icns": volumeIcon}

func init() { mountlib.AddRc("dokky", mountNFS) }

func mountHost() string {
	if platform.FinderHostReady() {
		return platform.FinderHost
	}
	return "localhost"
}

func mountNFS(VFS *vfs.VFS, mountpoint string, opt *mountlib.Options) (<-chan error, func() error, string, error) {
	h, err := nfs.NewHandler(context.Background(), VFS, &nfs.Opt)
	if err != nil {
		return nil, nil, "", err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, "", err
	}
	errs := make(chan error, 2)
	go func() {
		err := gonfs.Serve(l, newIconHandler(h))
		if errors.Is(err, net.ErrClosed) {
			err = nil
		}
		errs <- err
	}()
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	// locallocks: macOS only reads AppleDouble attributes (the mount icon) under a lock, and the server has no lock service
	args := []string{"-o", "port=" + port, "-o", "mountport=" + port, "-o", "tcp", "-o", "locallocks"}
	for _, o := range opt.ExtraOptions {
		args = append(args, "-o", o)
	}
	args = append(args, mountHost()+":/", mountpoint)
	if out, err := exec.Command("mount", args...).CombinedOutput(); err != nil {
		_ = l.Close()
		return nil, nil, "", fmt.Errorf("%s: failed to mount NFS volume: %w", bytes.TrimSpace(out), err)
	}
	var gone atomic.Bool
	unmount := func() error {
		if gone.Load() {
			return nil
		}
		// not diskutil: it reads the volume, and macOS then asks for network volume access on dokky's behalf
		err := unix.Unmount(mountpoint, unix.MNT_FORCE)
		gone.Store(err == nil)
		_ = l.Close()
		VFS.Shutdown()
		if err != nil {
			return fmt.Errorf("failed to unmount NFS volume: %w", err)
		}
		return nil
	}
	nfs.OnUnmountFunc = func() {
		gone.Store(true)
		_ = l.Close()
		errs <- nil
		VFS.Shutdown()
	}
	return errs, unmount, mountpoint, nil
}

const maxListings = 64

type listing struct {
	path    string
	entries []os.FileInfo
}

// without a stable listing per verifier, deleting while macOS pages through a big folder ends the listing early
type iconHandler struct {
	gonfs.Handler
	mu       sync.Mutex
	next     uint64
	listings map[uint64]listing
	order    []uint64
}

func newIconHandler(h gonfs.Handler) *iconHandler {
	return &iconHandler{Handler: h, next: uint64(time.Now().UnixNano()), listings: map[uint64]listing{}}
}

func (h *iconHandler) VerifierFor(path string, contents []os.FileInfo) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	h.listings[h.next] = listing{path, contents}
	h.order = append(h.order, h.next)
	if len(h.order) > maxListings {
		delete(h.listings, h.order[0])
		h.order = h.order[1:]
	}
	return h.next
}

func (h *iconHandler) DataForVerifier(path string, v uint64) []os.FileInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	if l, ok := h.listings[v]; ok && l.path == path {
		return l.entries
	}
	return nil
}

func (h *iconHandler) Mount(ctx context.Context, c net.Conn, r gonfs.MountRequest) (gonfs.MountStatus, billy.Filesystem, []gonfs.AuthFlavor) {
	s, f, a := h.Handler.Mount(ctx, c, r)
	return s, wrapFS(f), a
}

func (h *iconHandler) Change(f billy.Filesystem) billy.Change { return h.Handler.Change(unwrapFS(f)) }

func (h *iconHandler) FSStat(ctx context.Context, f billy.Filesystem, s *gonfs.FSStat) error {
	return h.Handler.FSStat(ctx, unwrapFS(f), s)
}

func (h *iconHandler) ToHandle(f billy.Filesystem, p []string) []byte {
	return h.Handler.ToHandle(unwrapFS(f), p)
}

func (h *iconHandler) FromHandle(b []byte) (billy.Filesystem, []string, error) {
	f, p, err := h.Handler.FromHandle(b)
	return wrapFS(f), p, err
}

func (h *iconHandler) InvalidateHandle(f billy.Filesystem, b []byte) error {
	return h.Handler.InvalidateHandle(unwrapFS(f), b)
}

type iconFS struct{ billy.Filesystem }

func wrapFS(f billy.Filesystem) billy.Filesystem {
	if f == nil {
		return nil
	}
	return iconFS{f}
}

func unwrapFS(f billy.Filesystem) billy.Filesystem {
	if i, ok := f.(iconFS); ok {
		return i.Filesystem
	}
	return f
}

func rootIcon(name string) ([]byte, bool) {
	b, ok := rootIcons[strings.TrimPrefix(path.Clean("/"+name), "/")]
	return b, ok
}

// macOS stores xattrs and Finder view state in these on servers without xattr support
func junk(name string) bool {
	base := path.Base(name)
	return strings.HasPrefix(base, "._") || base == ".DS_Store"
}

func (f iconFS) Stat(name string) (os.FileInfo, error) {
	if b, ok := rootIcon(name); ok {
		return iconInfo{path.Base(name), int64(len(b))}, nil
	}
	return f.Filesystem.Stat(name)
}

func (f iconFS) Lstat(name string) (os.FileInfo, error) {
	if b, ok := rootIcon(name); ok {
		return iconInfo{path.Base(name), int64(len(b))}, nil
	}
	return f.Filesystem.Lstat(name)
}

func (f iconFS) Open(name string) (billy.File, error) { return f.OpenFile(name, os.O_RDONLY, 0) }

func (f iconFS) Create(name string) (billy.File, error) {
	return f.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
}

func (f iconFS) OpenFile(name string, flag int, perm os.FileMode) (billy.File, error) {
	if b, ok := rootIcon(name); ok {
		if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0 {
			return nil, os.ErrPermission
		}
		return iconFile{bytes.NewReader(b), name}, nil
	}
	if flag&os.O_CREATE != 0 && junk(name) {
		if _, err := f.Filesystem.Lstat(name); err != nil {
			return nil, os.ErrPermission
		}
	}
	return f.Filesystem.OpenFile(name, flag, perm)
}

func (f iconFS) ReadDir(name string) ([]os.FileInfo, error) {
	entries, err := f.Filesystem.ReadDir(name)
	if err != nil || strings.Trim(path.Clean("/"+name), "/") != "" {
		return entries, err
	}
	for _, n := range slices.Sorted(maps.Keys(rootIcons)) {
		entries = append(entries, iconInfo{n, int64(len(rootIcons[n]))})
	}
	return entries, nil
}

func (f iconFS) Rename(from, to string) error {
	_, a := rootIcon(from)
	_, b := rootIcon(to)
	if a || b || junk(to) {
		return os.ErrPermission
	}
	return f.Filesystem.Rename(from, to)
}

func (f iconFS) Remove(name string) error {
	if _, ok := rootIcon(name); ok {
		return os.ErrPermission
	}
	return f.Filesystem.Remove(name)
}

var iconTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type iconInfo struct {
	name string
	size int64
}

func (i iconInfo) Name() string       { return i.name }
func (i iconInfo) Size() int64        { return i.size }
func (i iconInfo) Mode() os.FileMode  { return 0o444 }
func (i iconInfo) ModTime() time.Time { return iconTime }
func (i iconInfo) IsDir() bool        { return false }
func (i iconInfo) Sys() any {
	h := fnv.New64()
	_, _ = h.Write([]byte(i.name))
	return &file.FileInfo{Nlink: 1, UID: uint32(os.Getuid()), GID: uint32(os.Getgid()), Fileid: h.Sum64()}
}

type iconFile struct {
	*bytes.Reader
	name string
}

func (f iconFile) Name() string              { return f.name }
func (f iconFile) Write([]byte) (int, error) { return 0, os.ErrPermission }
func (f iconFile) Close() error              { return nil }
func (f iconFile) Lock() error               { return nil }
func (f iconFile) Unlock() error             { return nil }
func (f iconFile) Truncate(int64) error      { return os.ErrPermission }
