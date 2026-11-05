package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/itzzritik/dokky/cli/internal/store"
	_ "github.com/rclone/rclone/backend/all"
	_ "github.com/rclone/rclone/backend/local"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/cache"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/operations"
	"github.com/rclone/rclone/lib/atexit"
	"github.com/rclone/rclone/lib/diskusage"
	"github.com/rclone/rclone/librclone/librclone"
)

var (
	initOnce sync.Once
	running  atomic.Bool
)

type Engine struct {
	cacheDir string
	mem      *memConfig
	opMu     sync.Mutex // serialises Mount, Unmount, Stop and fs cache clearing
	stopped  bool
	mu       sync.Mutex
	mounted  map[string]liveMount // by mount id
	ejected  chan struct{}
}

// fires when the OS unmounts a mount behind dokky's back, e.g. a Finder eject
func (e *Engine) Ejected() <-chan struct{} { return e.ejected }

type liveMount struct {
	fs, point, serviceID string
	onUnmount            func()
}

var (
	takeUnmountHook = func(*Engine) func() { return nil }
	partialHash     = regexp.MustCompile(`^\.[0-9a-f]{8}$`)
)

type MountStats struct {
	ID                               string
	CacheBytes                       int64
	UploadsQueued, UploadsInProgress int
	FreeBytes                        uint64
}

type QueueItem struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Expiry    time.Time `json:"expiry"`
	Uploading bool      `json:"uploading"`
}

func Start(cacheDir string, onToken func(serviceID, token string)) (e *Engine, err error) {
	if !running.CompareAndSwap(false, true) {
		return nil, errors.New("rclone engine is already running in this process")
	}
	defer func() {
		if err != nil {
			running.Store(false)
		}
	}()
	initOnce.Do(func() {
		librclone.Initialize()
		atexit.IgnoreSignals()
	})
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, err
	}
	mem := newMemConfig(onToken)
	// SetData is a no-op while the config path is empty; memConfig never writes this file
	if err := config.SetConfigPath(filepath.Join(cacheDir, "rclone.conf")); err != nil {
		return nil, err
	}
	config.SetData(mem)
	if err := config.SetCacheDir(cacheDir); err != nil {
		return nil, err
	}
	main := map[string]any{
		"Transfers":       4,
		"DisableHTTP2":    true, // h2 squeezes parallel streams into one TCP connection
		"Timeout":         "30s",
		"ConnectTimeout":  "10s",
		"MaxBufferMemory": "512M",
	}
	if err := rpc("options/set", map[string]any{"main": main}, nil); err != nil {
		return nil, err
	}
	return &Engine{cacheDir: cacheDir, mem: mem, mounted: map[string]liveMount{}, ejected: make(chan struct{}, 1)}, nil
}

func (e *Engine) PutService(s store.Service) error {
	if _, err := fs.Find(s.Type); err != nil {
		return err
	}
	e.mem.PutService(s, isPassword)
	return nil
}

func (e *Engine) DeleteService(id string) { e.mem.DeleteService(id) }

func (e *Engine) Mount(m store.Mount, mountPoint string) (string, error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if e.stopped {
		return "", errors.New("rclone engine is stopped")
	}
	if err := e.prune(); err != nil {
		return "", err
	}
	e.mu.Lock()
	_, ok := e.mounted[m.ID]
	e.mu.Unlock()
	if ok {
		return "", fmt.Errorf("%s is already mounted", m.Name)
	}
	e.clearFsCache(m.ServiceID)
	fsName := m.ServiceID + ":" + m.RemotePath
	useServerSpace(context.Background(), fsName)
	var out struct{ MountPoint string }
	name := Title(m.Name)
	labelShare(name)
	if err := rpc("mount/mount", MountParams(fsName, mountPoint, name, m.VFS, runtime.GOOS), &out); err != nil {
		return "", err
	}
	e.mu.Lock()
	e.mounted[m.ID] = liveMount{fs: fsName, point: out.MountPoint, serviceID: m.ServiceID, onUnmount: takeUnmountHook(e)}
	e.mu.Unlock()
	return out.MountPoint, nil
}

func (e *Engine) Unmount(id string) error {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	return e.unmount(id)
}

func (e *Engine) unmount(id string) error {
	if err := e.prune(); err != nil {
		return err
	}
	// out of e.mounted first, so the eject watcher doesn't take our own unmount for a Finder eject
	e.mu.Lock()
	lm, ok := e.mounted[id]
	delete(e.mounted, id)
	e.mu.Unlock()
	if !ok {
		return nil
	}
	if err := rpc("mount/unmount", map[string]any{"mountPoint": lm.point}, nil); err != nil {
		e.mu.Lock()
		e.mounted[id] = lm
		e.mu.Unlock()
		return err
	}
	return nil
}

func livePoints() (map[string]bool, error) {
	var out struct{ MountPoints []struct{ MountPoint string } }
	if err := rpc("mount/listmounts", nil, &out); err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, m := range out.MountPoints {
		live[m.MountPoint] = true
	}
	return live, nil
}

// prune forgets mounts rclone no longer has, e.g. ejected in Finder
func (e *Engine) prune() error {
	live, err := livePoints()
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for name, lm := range e.mounted {
		if !live[lm.point] {
			delete(e.mounted, name)
		}
	}
	return nil
}

// clearFsCache drops rclone's cached Fs so changed config applies, unless a live mount still uses it
func (e *Engine) clearFsCache(serviceID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, lm := range e.mounted {
		if lm.serviceID == serviceID {
			return
		}
	}
	cache.ClearConfig(serviceID)
}

func (e *Engine) snapshot() map[string]liveMount {
	e.mu.Lock()
	defer e.mu.Unlock()
	return maps.Clone(e.mounted)
}

func (e *Engine) Stats() ([]MountStats, error) {
	live := e.snapshot()
	if len(live) == 0 {
		return nil, nil
	}
	points, err := livePoints()
	if err != nil {
		return nil, err
	}
	du, err := diskusage.New(e.cacheDir)
	if err != nil {
		return nil, err
	}
	stats := make([]MountStats, 0, len(live))
	for _, id := range slices.Sorted(maps.Keys(live)) {
		if !points[live[id].point] {
			continue
		}
		var out struct {
			DiskCache struct {
				BytesUsed                        int64
				UploadsQueued, UploadsInProgress int
			}
		}
		if err := rpc("vfs/stats", map[string]any{"fs": live[id].fs}, &out); err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		stats = append(stats, MountStats{
			ID:                id,
			CacheBytes:        out.DiskCache.BytesUsed,
			UploadsQueued:     out.DiskCache.UploadsQueued,
			UploadsInProgress: out.DiskCache.UploadsInProgress,
			FreeBytes:         du.Available,
		})
	}
	return stats, nil
}

func (e *Engine) Queue(id string) ([]QueueItem, error) {
	lm, ok := e.snapshot()[id]
	if !ok {
		return nil, fmt.Errorf("%s is not mounted", id)
	}
	var out struct {
		Queue []struct {
			Name      string
			Size      int64
			Expiry    float64
			Uploading bool
		}
	}
	if err := rpc("vfs/queue", map[string]any{"fs": lm.fs}, &out); err != nil {
		return nil, err
	}
	now := time.Now()
	items := make([]QueueItem, 0, len(out.Queue))
	for _, q := range out.Queue {
		items = append(items, QueueItem{
			Name:      q.Name,
			Size:      q.Size,
			Expiry:    now.Add(time.Duration(q.Expiry * float64(time.Second))),
			Uploading: q.Uploading,
		})
	}
	return items, nil
}

func (e *Engine) TotalBytes() (int64, error) {
	var out struct{ Bytes int64 }
	err := rpc("core/stats", nil, &out)
	return out.Bytes, err
}

func (e *Engine) StopJobs() error {
	var out struct {
		RunningIDs []int64 `json:"runningIds"`
	}
	if err := rpc("job/list", nil, &out); err != nil {
		return err
	}
	var errs []error
	for _, id := range out.RunningIDs {
		if err := rpc("job/stop", map[string]any{"jobid": id}, nil); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (e *Engine) RemovePartials(ctx context.Context, m store.Mount, names []string) error {
	fsName := m.ServiceID + ":" + m.RemotePath
	suffix := fs.GetConfig(ctx).PartialSuffix
	byDir := map[string][]string{}
	for _, n := range names {
		p := partialPrefix(n, len(".00000000")+len(suffix))
		byDir[path.Dir(p)] = append(byDir[path.Dir(p)], p)
	}
	var errs []error
	for dir, prefixes := range byDir {
		if dir == "." {
			dir = ""
		}
		var out struct{ List []struct{ Path string } }
		in := map[string]any{"fs": fsName, "remote": dir, "opt": map[string]any{"filesOnly": true, "noModTime": true, "noMimeType": true}}
		err := runJob(ctx, "operations/list", in, &out)
		if err != nil && !strings.Contains(err.Error(), fs.ErrorDirNotFound.Error()) {
			errs = append(errs, err)
		}
		for _, f := range out.List {
			if !slices.ContainsFunc(prefixes, func(p string) bool { return isPartial(f.Path, p, suffix) }) {
				continue
			}
			var done struct{}
			if err := runJob(ctx, "operations/deletefile", map[string]any{"fs": fsName, "remote": f.Path}, &done); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// rclone uploads to "<name>.<8 hex>.partial", cutting a name over 100 bytes (fs/operations/copy.go checkPartial)
func partialPrefix(name string, suffixLen int) string {
	if len(path.Base(name)) > 100 {
		return operations.TruncateString(name, len(name)-suffixLen)
	}
	return name
}

func isPartial(file, prefix, suffix string) bool {
	rest, ok := strings.CutPrefix(file, prefix)
	hash, cut := strings.CutSuffix(rest, suffix)
	return ok && cut && partialHash.MatchString(hash)
}

func (e *Engine) Stop() {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if e.stopped {
		return
	}
	e.stopped = true
	for id := range e.snapshot() {
		_ = e.unmount(id)
	}
	librclone.Finalize()
	running.Store(false)
}

func rpc(method string, in, out any) error {
	var input string
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		input = string(b)
	}
	res, status := librclone.RPC(method, input)
	if status != http.StatusOK {
		var e struct{ Error string }
		if json.Unmarshal([]byte(res), &e) != nil || e.Error == "" {
			e.Error = fmt.Sprintf("status %d", status)
		}
		return fmt.Errorf("%s: %s", method, e.Error)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal([]byte(res), out)
}

func (e *Engine) CleanupUploads(serviceID string) error {
	return rpc("backend/command", map[string]any{"command": "cleanup", "fs": serviceID + ":", "opt": map[string]string{"max-age": "24h"}}, nil)
}
