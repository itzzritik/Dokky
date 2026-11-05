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
)

const (
	historyLen = 120
	earlierLen = 50
	heartbeat  = 5 * time.Second
	spaceEvery = 30 * time.Second
)

type activity struct {
	mu       sync.Mutex
	bytes    int64
	at, sent time.Time
	done     map[string]int64
	cur      api.Activity
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

// reports whether to publish: every second while bytes move and once when they stop, else a heartbeat
func (d *Daemon) observe(now time.Time) bool {
	bytes, moving, err := d.eng.Sample()
	if err != nil {
		return false
	}
	finished, _ := d.eng.Finished()
	keys := d.liveKeys()
	queued := map[string][]engine.QueueItem{}
	for key, id := range keys {
		if items, err := d.eng.Queue(key); err == nil {
			queued[id] = items
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
		cur.Now = append(cur.Now, api.Transfer{DriveID: id, Name: m.Name, Up: m.Up, Size: m.Size, Done: m.Done, ETA: m.ETA})
	}
	// a transfer that ended between samples still moved its last bytes in this one
	for _, f := range finished {
		id, ok := keys[f.ID]
		if !ok || first || dt <= 0 || !f.At.After(prev) {
			continue
		}
		speed := float64(max(0, f.Size-a.done[fmt.Sprint(id, "\x00", f.Up, "\x00", f.Name)])) / dt
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
		if !ok || seen[k] || len(cur.Earlier) == earlierLen {
			continue
		}
		seen[k] = true
		t := api.Transfer{DriveID: id, Name: f.Name, Up: f.Up, Size: f.Size, Done: f.Size, At: f.At}
		if f.Error != "" {
			t.Error, t.RetryAt = logx.Redact(f.Error), retry[id+"\x00"+f.Name]
		}
		cur.Earlier = append(cur.Earlier, t)
	}

	cur.History = keep(a.cur.History, api.Sample{At: now, Down: cur.Down, Up: cur.Up})
	for _, id := range keys {
		cur.ByDrive[id] = keep(a.cur.ByDrive[id], api.Sample{At: now, Down: perDown[id], Up: perUp[id]})
	}
	was := a.cur.Up+a.cur.Down > 0
	a.cur = cur
	if was || total > 0 || len(moving) > 0 || now.Sub(a.sent) >= heartbeat {
		a.sent = now
		return true
	}
	return false
}

func keep(h []api.Sample, s api.Sample) []api.Sample {
	h = append(h, s)
	return slices.Clone(h[max(0, len(h)-historyLen):])
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
		return nil, fmt.Errorf("connect the drive to retry %s", p.Name)
	}
	return nil, d.eng.Retry(key, p.Name)
}

func (d *Daemon) refreshSpace(now time.Time) {
	type due struct{ id, key string }
	var list []due
	d.mu.Lock()
	for id, st := range d.states {
		switch {
		case st.state != stateMounted:
			delete(d.spaceAt, id)
			delete(d.space, id)
		case now.Sub(d.spaceAt[id]) >= spaceEvery:
			d.spaceAt[id] = now
			list = append(list, due{id, st.name})
		}
	}
	d.mu.Unlock()
	for _, x := range list {
		go func() {
			free, total, ok := d.eng.Space(x.key)
			d.mu.Lock()
			old, had := d.space[x.id]
			if ok {
				d.space[x.id] = api.Space{Free: free, Total: total}
			} else {
				delete(d.space, x.id)
			}
			changed := had != ok || old != d.space[x.id]
			d.mu.Unlock()
			if changed {
				d.publish()
			}
		}()
	}
}
