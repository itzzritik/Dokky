package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/logx"
	"github.com/itzzritik/dokky/cli/internal/store"
)

const (
	earlierLen = 50
	heartbeat  = 5 * time.Second
	spaceEvery = 30 * time.Second
	// a drive that isn't connected has no open session, and some services sign in again for each check
	spaceIdle  = 15 * time.Minute
	spaceRetry = time.Minute
)

type activity struct {
	mu       sync.Mutex
	bytes    int64
	at, sent time.Time
	done     map[string]int64
	cur      api.Activity
	lastRead map[string]time.Time
	lastUp   map[string]time.Time
	// a drive whose upload queue couldn't be read counts as busy
	queueErr map[string]time.Time
}

type retryParams struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (d *Daemon) sample(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if d.observe(now) && d.srv != nil {
				raw, _ := json.Marshal(d.activity())
				d.srv.Publish(ipc.Event{Event: api.EActivity, Data: raw})
			}
			d.refreshSpace(now)
		}
	}
}

func (d *Daemon) liveKeys() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	keys := map[string]string{}
	for id, st := range d.states {
		if st.state == stateMounted {
			keys[st.name] = id
		}
	}
	return keys
}

// reports whether to publish: every second while the last two minutes moved anything, else a heartbeat
func (d *Daemon) observe(now time.Time) bool {
	bytes, moving, err := d.eng.Sample()
	if err != nil {
		return false
	}
	finished, _ := d.eng.Finished()
	keys := d.liveKeys()
	queued := map[string][]engine.QueueItem{}
	var unread []string
	for key, id := range keys {
		if items, err := d.eng.Queue(key); err == nil {
			queued[id] = items
		} else {
			unread = append(unread, id)
		}
	}

	a := &d.act
	a.mu.Lock()
	defer a.mu.Unlock()
	dt, prev := now.Sub(a.at).Seconds(), a.at
	first := a.at.IsZero()
	var total float64
	if !first && dt > 0 && bytes >= a.bytes {
		total = float64(bytes-a.bytes) / dt
	}
	a.bytes, a.at = bytes, now

	done := map[string]int64{}
	up, perUp, perDown := 0.0, map[string]float64{}, map[string]float64{}
	cur := api.Activity{Now: []api.Transfer{}, Earlier: []api.Transfer{}, ByDrive: map[string][]api.Sample{}}
	for _, m := range moving {
		id, ok := keys[m.ID]
		if !ok {
			continue
		}
		k := fmt.Sprint(id, "\x00", m.Up, "\x00", m.Name)
		done[k] = m.Done
		var speed float64
		if !first && dt > 0 {
			speed = max(0, float64(m.Done-a.done[k])/dt)
		}
		if m.Up {
			up += speed
			perUp[id] += speed
		} else {
			perDown[id] += speed
		}
		done := m.Done
		if m.Size > 0 {
			// rclone counts bytes once buffered, which can run past the size
			done = min(done, m.Size)
		}
		cur.Now = append(cur.Now, api.Transfer{DriveID: id, Name: m.Name, Up: m.Up, Size: m.Size, Done: done, ETA: m.ETA})
	}
	// a transfer that ended between samples still moved its last bytes in this one
	for _, f := range finished {
		id, ok := keys[f.ID]
		if !ok || first || dt <= 0 || !f.At.After(prev) {
			continue
		}
		// bytes it really sent: a server that already has the file finishes without any
		sent := f.Bytes
		if f.Size >= 0 {
			sent = min(sent, f.Size)
		}
		speed := float64(max(0, sent-a.done[fmt.Sprint(id, "\x00", f.Up, "\x00", f.Name)])) / dt
		if f.Up {
			up += speed
			perUp[id] += speed
		} else {
			perDown[id] += speed
		}
	}
	a.done = done
	cur.Up, cur.Down = up, max(0, total-up)
	// VFS reads don't always list a transfer; with one drive the rest is its own
	if len(keys) == 1 {
		for _, id := range keys {
			perDown[id] = max(perDown[id], cur.Down)
		}
	}

	retry := map[string]time.Time{}
	for _, id := range slices.Sorted(maps.Keys(queued)) {
		for _, q := range queued[id] {
			retry[id+"\x00"+q.Name] = q.Expiry
			if !q.Uploading {
				cur.Now = append(cur.Now, api.Transfer{DriveID: id, Name: q.Name, Up: true, Size: q.Size, Waiting: true, RetryAt: q.Expiry})
			}
		}
	}
	seen := map[string]bool{}
	slices.SortStableFunc(finished, func(x, y engine.Finished) int { return y.At.Compare(x.At) })
	for _, f := range finished {
		id, ok := keys[f.ID]
		k := fmt.Sprint(id, "\x00", f.Up, "\x00", f.Name)
		// another read of the same file is still going, so it isn't done yet
		_, moving := done[k]
		if !ok || seen[k] || moving || len(cur.Earlier) == earlierLen {
			continue
		}
		seen[k] = true
		t := api.Transfer{DriveID: id, Name: f.Name, Up: f.Up, Size: f.Size, Done: f.Size, At: f.At}
		if f.Error != "" {
			t.Error, t.RetryAt = logx.Redact(f.Error), retry[id+"\x00"+f.Name]
		}
		cur.Earlier = append(cur.Earlier, t)
	}

	if a.lastRead == nil {
		a.lastRead, a.queueErr, a.lastUp = map[string]time.Time{}, map[string]time.Time{}, map[string]time.Time{}
	}
	for _, id := range unread {
		a.queueErr[id] = now
	}
	for id, down := range perDown {
		if down > 0 {
			a.lastRead[id] = now
		}
	}
	for id, sent := range perUp {
		if sent > 0 {
			a.lastUp[id] = now
		}
	}
	cur.History = keep(a.cur.History, api.Sample{At: now, Down: cur.Down, Up: cur.Up})
	for _, id := range keys {
		cur.ByDrive[id] = keep(a.cur.ByDrive[id], api.Sample{At: now, Down: perDown[id], Up: perUp[id]})
	}
	// every second while the graph still shows something, so it keeps scrolling
	shown := false
	for _, x := range cur.History {
		shown = shown || x.Down+x.Up > 0
	}
	a.cur = cur
	if shown || len(moving) > 0 || now.Sub(a.sent) >= heartbeat {
		a.sent = now
		return true
	}
	return false
}

func keep(h []api.Sample, s api.Sample) []api.Sample {
	h = append(h, s)
	return slices.Clone(h[max(0, len(h)-api.HistoryLen):])
}

func (d *Daemon) activity() api.Activity {
	d.act.mu.Lock()
	defer d.act.mu.Unlock()
	a := d.act.cur
	a.ByDrive = maps.Clone(a.ByDrive)
	if a.ByDrive == nil {
		a.ByDrive = map[string][]api.Sample{}
	}
	for _, s := range []*[]api.Transfer{&a.Now, &a.Earlier} {
		if *s == nil {
			*s = []api.Transfer{}
		}
	}
	if a.History == nil {
		a.History = []api.Sample{}
	}
	return a
}

func (d *Daemon) retry(p retryParams) (any, error) {
	d.mu.Lock()
	st := d.states[p.ID]
	var key string
	if st != nil && st.state == stateMounted {
		key = st.name
	}
	d.mu.Unlock()
	if key == "" {
		return nil, fmt.Errorf("mount the drive to retry %s", p.Name)
	}
	return nil, d.eng.Retry(key, p.Name)
}

func (d *Daemon) refreshSpace(now time.Time) {
	type due struct {
		m     store.Mount
		every time.Duration
	}
	var list []due
	d.mu.Lock()
	kept := map[string]bool{}
	for _, m := range d.data.Mounts {
		kept[m.ID] = true
		st := d.states[m.ID]
		if d.data.Local[m.ID].Removing || st != nil && st.state == stateMounting || now.Before(d.spaceAt[m.ID]) {
			continue
		}
		x := due{m: m, every: spaceIdle}
		if st != nil && st.state == stateMounted {
			x.every = spaceEvery
		}
		d.spaceAt[m.ID] = now.Add(x.every)
		list = append(list, x)
	}
	for id := range d.spaceAt {
		if !kept[id] {
			delete(d.space, id)
			delete(d.spaceAt, id)
			delete(d.spaceFails, id)
		}
	}
	d.mu.Unlock()
	for _, x := range list {
		m := x.m
		go func() {
			free, total, ok := d.eng.Space(m)
			d.mu.Lock()
			if !ok {
				// a failed check keeps the last answer; with none yet it asks again sooner, backing off to the usual wait,
				// since an unmounted check signs in again and MEGA blocks accounts that sign in too often
				if _, had := d.space[m.ID]; !had {
					d.spaceFails[m.ID] = min(d.spaceFails[m.ID]+1, 5)
					d.spaceAt[m.ID] = time.Now().Add(min(x.every, spaceRetry<<(d.spaceFails[m.ID]-1)))
				}
				d.mu.Unlock()
				return
			}
			delete(d.spaceFails, m.ID)
			old, had := d.space[m.ID]
			sp := api.Space{Free: free, Total: total}
			d.space[m.ID] = sp
			d.mu.Unlock()
			if !had || old != sp {
				d.publish()
			}
		}()
	}
}
