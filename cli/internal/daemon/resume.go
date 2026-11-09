package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	resumeFile = "resume.json"
	// a restart comes back in seconds; an older note is from a crash long ago and no longer what the user wants
	resumeMaxAge = 15 * time.Minute
)

type resumeNote struct {
	At     time.Time `json:"at"`
	Mounts []string  `json:"mounts"`
}

// before a restart: the drives that are up or meant to be, so the next start brings back exactly these, whatever happened to their flags meanwhile
func (d *Daemon) noteResume(state string) {
	if state == "" {
		return
	}
	d.mu.Lock()
	var ids []string
	for _, m := range d.data.Mounts {
		if d.data.Local[m.ID].Mounted || d.states[m.ID] != nil && d.states[m.ID].state == stateMounted {
			ids = append(ids, m.ID)
		}
	}
	d.mu.Unlock()
	raw, err := json.Marshal(resumeNote{At: time.Now(), Mounts: ids})
	if err == nil {
		err = writeAtomic(state, resumeFile, string(raw))
	}
	if err != nil {
		d.log.Warn("note drives to mount again", "err", err)
	}
}

// flags the drives a recent restart noted, then forgets the note; a drive removed or erased meanwhile stays down
func (d *Daemon) applyResume(state string, now time.Time) {
	if state == "" {
		return
	}
	path := filepath.Join(state, resumeFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	defer os.Remove(path)
	var n resumeNote
	if err != nil || json.Unmarshal(raw, &n) != nil || now.Sub(n.At) > resumeMaxAge || n.At.After(now.Add(time.Minute)) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.data.Removal != nil {
		return
	}
	var back []string
	for _, m := range d.data.Mounts {
		lm := d.data.Local[m.ID]
		if slices.Contains(n.Mounts, m.ID) && !lm.Mounted && !lm.Removing {
			d.setMounted(m.ID, true)
			back = append(back, m.Name)
		}
	}
	if len(back) == 0 {
		return
	}
	d.log.Warn("drives were down after the restart, mounting them again", "drives", back)
	if err := d.save(); err != nil {
		d.log.Error("save store", "err", err)
	}
}
