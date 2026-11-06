package daemon

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/store"
)

const (
	readBusyFor = 10 * time.Second
	unsentFor   = 10 * time.Second
	// a failing upload gets this long to recover before it counts as stuck
	stuckAfter = 2 * time.Minute
)

const (
	usageIdle  = "idle"
	usageBusy  = "busy"
	usageStuck = "stuck"
)

type unsentCount struct {
	n  int
	at time.Time
}

func (d *Daemon) usage(id string) api.Usage { return d.usageOf(id, false) }

// never takes opMu, so it answers while a slow mount or unmount runs; draining waits on a drive that will remount
func (d *Daemon) usageOf(id string, draining bool) api.Usage {
	m, ok := d.findMount(byID(id))
	if !ok {
		return api.Usage{State: usageIdle}
	}
	d.mu.Lock()
	st := d.states[id]
	mounted := st != nil && st.state == stateMounted
	flagged := d.data.Local[id].Mounted
	d.mu.Unlock()

	d.act.mu.Lock()
	read, queueErr := d.act.lastRead[id], d.act.queueErr[id]
	failing := map[string]string{}
	for _, t := range d.act.cur.Earlier {
		if t.DriveID == id && t.Up && t.Error != "" {
			if _, seen := failing[t.Name]; !seen {
				failing[t.Name] = t.Error
			}
		}
	}
	var uploading, waiting int
	var stuckErr string
	allFailing, offline := true, false
	for _, t := range d.act.cur.Now {
		if t.DriveID != id || !t.Up {
			continue
		}
		if !t.Waiting {
			uploading++
			continue
		}
		waiting++
		if e, ok := failing[t.Name]; ok {
			stuckErr = cmp.Or(stuckErr, e)
			offline = offline || api.FailKind(e) == "offline"
		} else {
			allFailing = false
		}
	}
	d.act.mu.Unlock()

	now := time.Now()
	stuckFor := d.failingFor(id, mounted && uploading == 0 && waiting > 0 && allFailing && !offline, now)
	switch {
	case mounted && now.Sub(queueErr) < readBusyFor:
		return api.Usage{State: usageBusy, Reason: "Checking uploads"}
	case mounted && (uploading > 0 || waiting > 0 && !allFailing):
		return api.Usage{State: usageBusy, Reason: files(uploading+waiting) + " uploading"}
	case now.Sub(read) < readBusyFor:
		return api.Usage{State: usageBusy, Reason: "Reading files"}
	case mounted && d.unsentOf(m) > waiting:
		// rclone holds a file out of its queue while it is still being written
		return api.Usage{State: usageBusy, Reason: "Writing files"}
	case mounted && waiting > 0 && offline:
		return api.Usage{State: usageBusy, Reason: files(waiting) + " waiting for the network"}
	case mounted && waiting > 0 && stuckFor < stuckAfter:
		return api.Usage{State: usageBusy, Reason: files(waiting) + " retrying"}
	case mounted && waiting > 0:
		return api.Usage{State: usageStuck, Reason: fmt.Sprintf("%s can't upload: %s", files(waiting), stuckErr)}
	case !mounted:
		if n := d.unsentOf(m); n > 0 {
			// a drive that mounts again on its own uploads these, so a removal waits for it
			if draining && flagged {
				return api.Usage{State: usageBusy, Reason: files(n) + " not uploaded yet"}
			}
			return api.Usage{State: usageStuck, Reason: files(n) + " not uploaded yet"}
		}
	}
	return api.Usage{State: usageIdle}
}

// how long every upload of the drive has been failing, zero once one goes through
func (d *Daemon) failingFor(id string, failing bool, now time.Time) time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !failing {
		delete(d.stuckSince, id)
		return 0
	}
	since, ok := d.stuckSince[id]
	if !ok {
		d.stuckSince[id] = now
		return 0
	}
	return now.Sub(since)
}

// rel is <service id>/<remote path>/<file>
func covers(x store.Mount, rel string) bool {
	svc, file, _ := strings.Cut(rel, "/")
	root := strings.Trim(path.Clean("/"+x.RemotePath), "/")
	return x.ServiceID == svc && (root == "" || strings.HasPrefix(file, root+"/"))
}

// a cache file another drive of the account also shows is that drive's too, so it stays
func coveredByOther(m store.Mount, all []store.Mount, rel string) bool {
	return slices.ContainsFunc(all, func(x store.Mount) bool { return x.ID != m.ID && covers(x, rel) })
}

// files of this drive alone not uploaded yet; walking the cache is slow, so the count is kept for a few seconds
func (d *Daemon) unsentOf(m store.Mount) int {
	d.mu.Lock()
	c, ok := d.unsent[m.ID]
	all := slices.Clone(d.data.Mounts)
	d.mu.Unlock()
	if ok && time.Since(c.at) < unsentFor {
		return c.n
	}
	root := filepath.Join(d.cacheDir, "vfsMeta")
	n := 0
	err := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		switch {
		case errors.Is(err, os.ErrNotExist):
			return nil
		case err != nil || e.IsDir():
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel = filepath.ToSlash(rel); !covers(m, rel) || coveredByOther(m, all, rel) {
			return nil
		}
		if dirtyMeta(p) {
			n++
		}
		return nil
	})
	if err != nil {
		d.log.Warn("count unsent files", "drive", m.Name, "err", err)
	}
	d.mu.Lock()
	d.unsent[m.ID] = unsentCount{n, time.Now()}
	d.mu.Unlock()
	return n
}

func files(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// a person's unmount waits for this device's own transfers; other devices never block it
func (d *Daemon) unmountIdle(id string) error {
	if u := d.usage(id); u.State == usageBusy {
		m, _ := d.findMount(byID(id))
		return &codedError{"in_use", fmt.Sprintf("%s is in use: %s", m.Name, u.Reason)}
	}
	if err := d.unmount(id); err != nil {
		return err
	}
	if m, ok := d.findMount(byID(id)); ok {
		d.event("drive.unmounted", m.ID, m.Name, "", true)
	}
	return nil
}

func (d *Daemon) mountByPerson(id string) error {
	d.mu.Lock()
	recovering := d.state(id).logged
	d.mu.Unlock()
	if err := d.mount(id); err != nil {
		return err
	}
	// a drive that was failing already logged that it mounted again
	if m, ok := d.findMount(byID(id)); ok && !recovering {
		d.event("drive.mounted", m.ID, m.Name, "", true)
	}
	return nil
}

func dirtyMeta(p string) bool {
	b, err := os.ReadFile(p)
	var info struct{ Dirty bool }
	return err == nil && json.Unmarshal(b, &info) == nil && info.Dirty
}
