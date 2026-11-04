package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/fs"
)

const (
	uninstallAfter = 10 * time.Minute
	revokeTimeout  = 15 * time.Second
	partialTimeout = time.Minute
	shownNames     = 3
	installsFile   = "installs.json"
)

var removeFinderHost = platform.RemoveFinderHost

type Decision int

const (
	Keep Decision = iota
	DropRecord
	Wipe
)

type InstallWatch struct {
	since time.Time
	gone  map[string]time.Time
	known []string
	mtime time.Time
	drop  []string
}

func (w *InstallWatch) Check(now time.Time, records []string, exists func(string) bool) Decision {
	w.drop = nil
	live, present, fresh := false, false, false
	gone, stale := map[string]time.Time{}, []string(nil)
	for _, r := range records {
		fresh = fresh || !slices.Contains(w.known, r)
		root := globalModules(r)
		if !filepath.IsAbs(r) || !exists(root) {
			w.drop = append(w.drop, r)
			continue
		}
		live = true
		if exists(r) || exists(filepath.Join(root, "dokky")) {
			present = true
			continue
		}
		gone[r] = now
		if t, ok := w.gone[r]; ok {
			gone[r] = t
		}
		if now.Sub(gone[r]) >= uninstallAfter {
			stale = append(stale, r)
		}
	}
	w.known, w.gone = slices.Clone(records), gone
	// a removal while another install still has dokky is not an uninstall
	if present {
		w.drop = append(w.drop, stale...)
	}
	if !live || present || fresh {
		w.since = time.Time{}
	}
	if live && !present && w.since.IsZero() {
		w.since = now
	}
	switch {
	case len(w.drop) > 0:
		return DropRecord
	case live && !present && now.Sub(w.since) >= uninstallAfter:
		return Wipe
	}
	return Keep
}

func (w *InstallWatch) noteMtime(mtime time.Time) {
	if !mtime.Equal(w.mtime) {
		w.mtime, w.since = mtime, time.Time{}
	}
}

// the outermost node_modules, so pnpm's versioned store folders count as one install
func globalModules(record string) string {
	root := filepath.Dir(record)
	for dir := root; filepath.Dir(dir) != dir; dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "node_modules" {
			root = dir
		}
	}
	return root
}

// an unreadable folder counts as present
func pathExists(p string) bool {
	_, err := os.Stat(p)
	return !errors.Is(err, os.ErrNotExist)
}

func (d *Daemon) watchInstalls(ctx context.Context, wipe chan<- *InstallWatch) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		d.log.Warn("watch npm installs, checking every 30 seconds instead", "err", err)
	}
	defer func() {
		if fw != nil {
			_ = fw.Close()
		}
	}()
	tick := time.NewTicker(tickInterval)
	defer tick.Stop()
	var w InstallWatch
	for {
		if d.checkInstalls(&w, time.Now(), pathExists) == Wipe {
			select {
			case wipe <- &w:
			case <-ctx.Done():
			}
			return
		}
		var events <-chan fsnotify.Event
		var errs <-chan error
		if fw != nil {
			for _, r := range w.known {
				_ = fw.Add(globalModules(r))
			}
			events, errs = fw.Events, fw.Errors
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-events:
		case err := <-errs:
			d.log.Warn("watch npm installs, checking every 30 seconds instead", "err", err)
			_ = fw.Close()
			fw = nil
		}
	}
}

func (d *Daemon) checkInstalls(w *InstallWatch, now time.Time, exists func(string) bool) Decision {
	state := d.upd.paths.State
	final := false
	for {
		records, mtime, err := readInstalls(state)
		if err != nil {
			d.log.Warn("read npm install records", "err", err)
			return Keep
		}
		w.noteMtime(mtime)
		switch dec := w.Check(now, records, exists); dec {
		case DropRecord:
			d.log.Info("a Node version or package manager that had dokky is gone, forgetting it", "folders", w.drop)
			rest := slices.DeleteFunc(slices.Clone(records), func(r string) bool { return slices.Contains(w.drop, r) })
			if err := writeInstalls(state, rest, w); err != nil {
				d.log.Warn("write npm install records", "err", err)
				return Keep
			}
		case Wipe:
			if final {
				return Wipe
			}
			final = true
		default:
			return dec
		}
	}
}

func readInstalls(state string) ([]string, time.Time, error) {
	path := filepath.Join(state, installsFile)
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var records []string
	if err := json.Unmarshal(b, &records); err != nil {
		return nil, time.Time{}, fmt.Errorf("%s: %w", path, err)
	}
	return records, fi.ModTime(), nil
}

func writeInstalls(state string, records []string, w *InstallWatch) error {
	b, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(state, installsFile, string(b)); err != nil {
		return err
	}
	fi, err := os.Stat(filepath.Join(state, installsFile))
	if err != nil {
		return err
	}
	// dokky's own rewrite is not a launcher run
	w.mtime = fi.ModTime()
	return nil
}

func DeepClean(d *Daemon) {
	p := d.upd.paths
	d.log.Warn("dokky was removed with npm, uninstalling it")

	d.opMu.Lock()
	d.mu.Lock()
	acct, mounts := d.data.Account, slices.Clone(d.data.Mounts)
	live := map[string]string{}
	for _, m := range mounts {
		if st := d.states[m.ID]; st != nil && st.name != "" {
			live[m.ID] = st.name
		}
	}
	d.mu.Unlock()

	queued := map[string][]engine.QueueItem{}
	var stopped []engine.QueueItem
	for _, m := range mounts {
		if name, ok := live[m.ID]; ok {
			items, err := d.eng.Queue(name)
			if err != nil {
				d.log.Warn("read upload queue", "name", m.Name, "err", err)
			}
			queued[m.ID], stopped = items, append(stopped, items...)
		}
	}
	if err := d.eng.StopJobs(); err != nil {
		d.log.Warn("stop rclone jobs", "err", err)
	}
	// unmounting shuts each VFS down, which cancels its uploads
	d.releaseAll(mounts)
	d.removePartials(mounts, queued)

	d.mu.Lock()
	points := make([]string, 0, len(mounts))
	for _, m := range mounts {
		points = append(points, d.mountPoint(m))
	}
	d.mu.Unlock()
	for _, pt := range points {
		engine.CleanupMountPoint(pt)
	}
	removeEmptyDir(filepath.Join(d.home, "dokky"))

	if acct != nil {
		ctx, cancel := context.WithTimeout(context.Background(), revokeTimeout)
		if err := client(acct).Revoke(ctx, "self"); err != nil {
			d.log.Warn("revoke device", "err", err)
		}
		cancel()
		if err := d.forget(); err != nil {
			d.log.Error("delete store", "err", err)
		}
	} else {
		// a forced unmount leaves the flag set
		d.mu.Lock()
		for id := range d.data.Local {
			d.setMounted(id, false)
		}
		err := d.save()
		d.mu.Unlock()
		if err != nil {
			d.log.Error("save store", "err", err)
		}
	}
	d.opMu.Unlock()

	if err := d.unregister(); err != nil {
		d.log.Warn("unregister the dokky service", "err", err)
	}
	if err := removeFinderHost(); err != nil {
		d.log.Warn("remove the Dokky name from /etc/hosts", "err", err)
	}
	if d.ownsPath(p) {
		if err := d.removePath(d.home, p.State); err != nil {
			d.log.Warn("remove PATH entry", "err", err)
		}
	}
	files, dirs := ownedPaths(p)
	var left []string
	for _, path := range files {
		if fi, err := os.Lstat(path); err == nil && fi.IsDir() {
			d.log.Warn("a folder sits where a dokky file belongs, leaving it", "path", path)
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			left = append(left, path)
		}
	}
	for _, path := range dirs {
		if err := os.RemoveAll(path); err != nil {
			left = append(left, path)
		}
	}
	removeEmptyDir(p.State)

	if err := d.notify("dokky", uninstalledMessage(stopped)); err != nil {
		d.log.Warn("notify", "err", err)
	}
	if err := d.removeLater(left, p.State); err != nil {
		d.log.Warn("start the cleanup helper", "err", err)
	}
	// on macOS and Linux this stops the daemon
	if err := d.stopService(); err != nil {
		d.log.Warn("stop the dokky service", "err", err)
	}
}

// caller holds opMu
func (d *Daemon) removePartials(mounts []store.Mount, queued map[string][]engine.QueueItem) {
	ctx, cancel := context.WithTimeout(context.Background(), partialTimeout)
	defer cancel()
	for _, m := range mounts {
		var names []string
		for _, it := range queued[m.ID] {
			names = append(names, it.Name)
		}
		if len(names) == 0 {
			continue
		}
		if err := d.eng.RemovePartials(ctx, m, names); err != nil {
			d.log.Warn("remove half-uploaded files", "name", m.Name, "err", err)
		}
	}
}

func (d *Daemon) ownsPath(p Paths) bool {
	if _, err := os.Stat(filepath.Join(p.State, "path.json")); err != nil {
		return false
	}
	list, err := os.ReadDir(filepath.Join(d.home, ".local", "bin"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false
	}
	for _, e := range list {
		if e.Name() != filepath.Base(p.Bin) {
			return false
		}
	}
	return true
}

func ownedPaths(p Paths) (files, dirs []string) {
	if p.State == "" || p.Cache == "" {
		return nil, nil
	}
	if p.Bin != "" {
		files = append(files, p.Bin)
	}
	for _, name := range []string{"dokky.old", "version", "pin", installsFile, "path.json", "dokky.log", "daemon.lock"} {
		files = append(files, filepath.Join(p.State, name))
	}
	backups, _ := filepath.Glob(filepath.Join(p.State, "dokky-*.log"))
	files = append(files, backups...)
	if sock := platform.SocketAddr(p.State); filepath.Dir(sock) == p.State {
		files = append(files, sock)
	}
	return files, []string{p.Cache, filepath.Join(p.State, "updates")}
}

func removeEmptyDir(dir string) {
	if fi, err := os.Lstat(dir); err == nil && fi.IsDir() {
		_ = os.Remove(dir)
	}
}

func uninstalledMessage(stopped []engine.QueueItem) string {
	msg := "dokky was uninstalled."
	if len(stopped) == 0 {
		return msg
	}
	var size int64
	var names []string
	for i, it := range stopped {
		size += it.Size
		if i < shownNames {
			names = append(names, it.Name)
		}
	}
	list := strings.Join(names, ", ")
	if more := len(stopped) - len(names); more > 0 {
		list += fmt.Sprintf(" and %d more", more)
	}
	files, were := "files", "were"
	if len(stopped) == 1 {
		files, were = "file", "was"
	}
	return fmt.Sprintf("%s %d %s (%s) %s not uploaded: %s.", msg, len(stopped), files, fs.SizeSuffix(size).ByteUnit(), were, list)
}
