package daemon

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
)

const (
	stateUnmounted = "unmounted"
	stateMounting  = "mounting"
	stateMounted   = "mounted"
	stateError     = "error"
	stateRemoved   = "removed"

	commandTimeout = 20 * time.Second
	maxBackoffStep = 5
)

var (
	pollEvery   = 100 * time.Millisecond
	goneTimeout = 5 * time.Second
	// rclone's second unmount of a path could knock off a remount made right after
	remountSettle = 2 * time.Second
	retryBase     = 30 * time.Second
)

type mountEngine interface {
	Mount(store.Mount, string) (string, error)
	Unmount(string) error
	Reachable(store.Service, string) error
	PutService(store.Service) error
	DeleteService(string)
	Stats() ([]engine.MountStats, error)
	ConfigStep(ctx context.Context, name, typ string, opts map[string]string, state, result string) (engine.Step, error)
	TestRemote(store.Service) error
	ListDirs(store.Service, string) ([]string, error)
	Queue(string) ([]engine.QueueItem, error)
	TotalBytes() (int64, error)
	StopJobs() error
	RemovePartials(ctx context.Context, m store.Mount, names []string) error
	CleanupUploads(serviceID string) error
	Sample() (int64, []engine.Moving, error)
	Finished() ([]engine.Finished, error)
	Retry(id, name string) error
	Space(store.Mount) (free, total int64, ok bool)
}

type mountState struct {
	state, err  string
	name, point string
	failures    int
	retryAt     time.Time
	remount     bool // a pulled change waits for the next mount
}

func byID(id string) func(store.Mount) bool {
	return func(m store.Mount) bool { return m.ID == id }
}

func (d *Daemon) mount(id string) error {
	m, ok := d.findMount(byID(id))
	if !ok {
		return fmt.Errorf("no drive with id %q", id)
	}
	// the drive shows connecting from the press, not only once the remote has answered
	d.mu.Lock()
	if st := d.state(m.ID); st.state != stateMounted {
		st.state, st.err = stateMounting, ""
	}
	d.mu.Unlock()
	d.publish()
	if err := d.probe(m); err != nil {
		return err
	}
	d.opMu.Lock()
	defer d.opMu.Unlock()
	_ = d.refresh()
	if m, ok = d.findMount(byID(id)); !ok {
		return fmt.Errorf("no drive with id %q", id)
	}
	return d.mountLocked(m)
}

// runs without opMu so an offline server never blocks other commands
func (d *Daemon) probe(m store.Mount) error {
	d.mu.Lock()
	if d.state(m.ID).state == stateMounted {
		d.mu.Unlock()
		return nil
	}
	i := slices.IndexFunc(d.data.Services, func(s store.Service) bool { return s.ID == m.ServiceID })
	var svc store.Service
	if i >= 0 {
		svc = d.data.Services[i]
	}
	d.mu.Unlock()

	err := fmt.Errorf("no service with id %q", m.ServiceID)
	if i >= 0 {
		err = d.eng.Reachable(svc, m.RemotePath)
	}
	if err != nil {
		d.mu.Lock()
		if st := d.states[m.ID]; st != nil {
			d.fail(m, st, err)
		}
		d.mu.Unlock()
		d.publish()
	}
	return err
}

func (d *Daemon) mountLocked(m store.Mount) error {
	d.mu.Lock()
	st := d.state(m.ID)
	if st.state == stateMounted {
		d.mu.Unlock()
		return nil
	}
	point := d.mountPoint(m)
	for id, other := range d.states {
		// "*" lets Windows pick a free drive letter for each mount
		if id != m.ID && other.name != "" && strings.EqualFold(other.point, point) && point != "*" {
			d.mu.Unlock()
			return fmt.Errorf("mount point %s is used by another drive", point)
		}
	}
	// a stale mount is dokky's only if flagged or at the default path
	ours := d.data.Local[m.ID].Mounted || point == d.defaultPoint(m.Name)
	// a sign-in clash can move a live mount to a new id while rclone still holds the old one
	engineID := cmp.Or(st.name, m.ID)
	st.state, st.err = stateMounting, ""
	d.mu.Unlock()
	d.publish()

	actual, err := d.attach(m, engineID, point, ours)

	d.mu.Lock()
	if err != nil {
		d.fail(m, st, err)
	} else {
		*st = mountState{state: stateMounted, name: m.ID, point: actual}
		// a fresh mount asks for its space on the next tick, whatever an unmounted check scheduled
		delete(d.spaceAt, m.ID)
		delete(d.spaceFails, m.ID)
		d.setMounted(m.ID, true)
		err = d.save()
		go d.reportDrives()
	}
	d.mu.Unlock()
	d.publish()
	return err
}

func (d *Daemon) fail(m store.Mount, st *mountState, err error) {
	if st.state == stateMounted {
		return
	}
	st.state, st.err = stateError, err.Error()
	st.failures++
	st.retryAt = time.Now().Add(retryBase << min(st.failures-1, maxBackoffStep))
	d.log.Error("mount failed", "name", m.Name, "err", err)
}

func (d *Daemon) attach(m store.Mount, engineID, point string, ours bool) (string, error) {
	if err := engine.CheckDriver(); err != nil {
		return "", &codedError{"no_driver", err.Error()}
	}
	if err := d.clearEngine(engineID); err != nil {
		return "", err
	}
	if wait := remountSettle - time.Since(d.unmountedAt[point]); wait > 0 {
		time.Sleep(wait)
	}
	if d.isMounted(point) {
		if !ours {
			return "", fmt.Errorf("mount point %s is already in use, unmount it first", point)
		}
		if err := d.clearPoint(point); err != nil {
			return "", err
		}
	}
	if err := engine.PrepareMountPoint(point); err != nil {
		return "", err
	}
	return d.eng.Mount(m, point)
}

func (d *Daemon) unmount(id string) error {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	_ = d.refresh()
	m, ok := d.findMount(byID(id))
	if !ok {
		return fmt.Errorf("no drive with id %q", id)
	}
	return d.unmountLocked(m)
}

func (d *Daemon) unmountLocked(m store.Mount) error {
	d.mu.Lock()
	st := d.state(m.ID)
	live, flagged := st.name != "", d.data.Local[m.ID].Mounted
	engineID, point := cmp.Or(st.name, m.ID), cmp.Or(st.point, d.mountPoint(m))
	ours := live || point == d.defaultPoint(m.Name)
	d.mu.Unlock()

	var err error
	if live || flagged {
		err = d.detach(engineID, point, ours)
	}

	d.mu.Lock()
	if err != nil {
		st.state, st.err = stateError, err.Error()
	} else {
		*st = mountState{state: stateUnmounted}
		d.markUnmounted(m.ID)
		err = d.save()
		go d.reportDrives()
	}
	d.mu.Unlock()
	d.publish()
	return err
}

func (d *Daemon) detach(id, point string, ours bool) error {
	if err := d.clearEngine(id); err != nil {
		return err
	}
	if !ours {
		return nil
	}
	if !poll(func() bool { return !d.isMounted(point) }) {
		if err := d.clearPoint(point); err != nil {
			return err
		}
	}
	d.unmountedAt[point] = time.Now()
	engine.CleanupMountPoint(point)
	return nil
}

func (d *Daemon) clearEngine(id string) error {
	var err error
	if poll(func() bool { err = d.eng.Unmount(id); return err == nil }) {
		return nil
	}
	return err
}

func (d *Daemon) clearPoint(point string) error {
	d.log.Warn("force-unmounting a stale mount", "point", point)
	if err := d.forceUnmount(point); err != nil {
		d.log.Warn("force unmount failed", "point", point, "err", err)
	}
	if poll(func() bool { return !d.isMounted(point) }) {
		return nil
	}
	return fmt.Errorf("%s is still mounted and could not be force-unmounted", point)
}

func (d *Daemon) refresh() error {
	err := d.refreshStats()
	if err != nil {
		d.log.Warn("mount stats", "err", err)
	}
	return err
}

func (d *Daemon) observeIdle(now time.Time, statsErr error) {
	if statsErr != nil {
		d.idle.Unknown()
		return
	}
	total, err := d.eng.TotalBytes()
	if err != nil {
		d.log.Warn("total bytes", "err", err)
		d.idle.Unknown()
		return
	}
	d.mu.Lock()
	uploads := 0
	for _, s := range d.stats {
		uploads += s.UploadsQueued + s.UploadsInProgress
	}
	d.mu.Unlock()
	d.idle.Observe(now, uploads, total)
}

func (d *Daemon) refreshStats() error {
	stats, err := d.eng.Stats()
	if err != nil {
		return err
	}
	live := make(map[string]engine.MountStats, len(stats))
	for _, s := range stats {
		live[s.ID] = s
	}
	d.mu.Lock()
	d.stats = live
	var gone, names []string
	for id, st := range d.states {
		if _, ok := live[st.name]; st.state == stateMounted && !ok {
			gone = append(gone, st.point)
			if i := slices.IndexFunc(d.data.Mounts, byID(id)); i >= 0 {
				names = append(names, d.data.Mounts[i].Name)
			}
			*st = mountState{state: stateUnmounted}
			d.markUnmounted(id)
		}
	}
	if len(gone) > 0 {
		if err := d.save(); err != nil {
			d.log.Error("save store", "err", err)
		}
	}
	quiet := !d.data.Settings.NotifyDisconnect
	d.mu.Unlock()
	if len(names) > 0 && !quiet {
		slices.Sort(names)
		go func() {
			if err := d.notify("dokky", strings.Join(names, ", ")+" ejected. Open dokky to mount again."); err != nil {
				d.log.Warn("notify", "err", err)
			}
		}()
	}
	for _, p := range gone {
		engine.CleanupMountPoint(p)
	}
	return nil
}

func (d *Daemon) remountFlagged() {
	now := time.Now()
	d.mu.Lock()
	var due []store.Mount
	for _, m := range d.data.Mounts {
		if st := d.state(m.ID); d.data.Local[m.ID].Mounted && st.state != stateMounted && !now.Before(st.retryAt) {
			due = append(due, m)
		}
	}
	d.mu.Unlock()
	for _, m := range due {
		if d.probe(m) != nil {
			continue
		}
		d.opMu.Lock()
		if cur, ok := d.findMount(byID(m.ID)); ok && d.flagged(m.ID) {
			_ = d.mountLocked(cur)
		}
		d.opMu.Unlock()
	}
}

// a change pulled from another device waits on a live mount until nothing uploads, then remounts on its own
func (d *Daemon) applyPulled() {
	d.mu.Lock()
	var due []string
	for id, st := range d.states {
		if s := d.stats[st.name]; st.state == stateMounted && st.remount && s.UploadsQueued+s.UploadsInProgress == 0 {
			due = append(due, id)
		}
	}
	d.mu.Unlock()
	for _, id := range due {
		d.mu.Lock()
		flagged := d.data.Local[id].Mounted
		d.mu.Unlock()
		if d.unmount(id) != nil {
			continue
		}
		err := d.mount(id)
		d.mu.Lock()
		// a failed mount stays flagged, so the next tick tries again
		d.setMounted(id, flagged || err != nil)
		if serr := d.save(); serr != nil {
			d.log.Error("save store", "err", serr)
		}
		d.mu.Unlock()
		d.publish()
	}
}

func (d *Daemon) findMount(match func(store.Mount) bool) (store.Mount, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := slices.IndexFunc(d.data.Mounts, match)
	if i < 0 {
		return store.Mount{}, false
	}
	return d.data.Mounts[i], true
}

func (d *Daemon) flagged(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.data.Local[id].Mounted
}

func (d *Daemon) state(id string) *mountState {
	st, ok := d.states[id]
	if !ok {
		st = &mountState{state: stateUnmounted}
		d.states[id] = st
	}
	return st
}

func (d *Daemon) setMounted(id string, on bool) {
	lm := d.data.Local[id]
	lm.Mounted = on
	d.data.Local[id] = lm
}

// caller holds mu; a mount deleted on another device goes once it is unmounted here
func (d *Daemon) markUnmounted(id string) {
	d.setMounted(id, false)
	if d.data.Local[id].RemovedRemotely {
		d.data = dsync.DropRemoved(d.data, id)
		delete(d.states, id)
	}
}

func (d *Daemon) defaultPoint(name string) string {
	return engine.DefaultMountPoint(d.home, engine.Title(name), runtime.GOOS)
}

func (d *Daemon) mountPoint(m store.Mount) string {
	if p := d.data.Local[m.ID].MountPoint; p != "" {
		return p
	}
	return d.defaultPoint(m.Name)
}

func poll(ok func() bool) bool {
	deadline := time.Now().Add(goneTimeout)
	for !ok() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pollEvery)
	}
	return true
}

func forceUnmount(point string) error {
	switch runtime.GOOS {
	case "darwin":
		if run("diskutil", "unmount", "force", point) == nil {
			return nil
		}
		return run("umount", "-f", point)
	case "linux":
		return run("fusermount3", "-uz", point)
	}
	return nil
}

func run(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, bytes.TrimSpace(out))
	}
	return nil
}

const cleanupEvery = 24 * time.Hour

// cancelled multipart uploads can leave their parts on the server; sweep them like an S3 lifecycle rule
func (d *Daemon) cleanupUploads(now time.Time) {
	d.mu.Lock()
	var due []string
	for _, s := range d.data.Services {
		if s.Type == "s3" && now.Sub(d.cleaned[s.ID]) >= cleanupEvery {
			d.cleaned[s.ID] = now
			due = append(due, s.ID)
		}
	}
	d.mu.Unlock()
	for _, id := range due {
		go func() {
			if err := d.eng.CleanupUploads(id); err != nil {
				d.log.Warn("clean up unfinished uploads", "service", id, "err", err)
			}
		}()
	}
}
