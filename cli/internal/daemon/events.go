package daemon

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/logx"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
)

const (
	eventsKept   = 90 * 24 * time.Hour
	eventsQueue  = 500
	eventsBatch  = 50
	eventsShown  = 100
	eventTextMax = 300
)

func clip(s string) string {
	if r := []rune(s); len(r) > eventTextMax {
		return string(r[:eventTextMax-1]) + "…"
	}
	return s
}

// caller must not hold mu
func (d *Daemon) event(kind, target, name, reason string, ok bool) {
	d.mu.Lock()
	d.eventLocked(kind, target, name, reason, ok)
	d.mu.Unlock()
}

// caller holds mu
func (d *Daemon) eventLocked(kind, target, name, reason string, ok bool) {
	e := api.Event{ID: uuid.NewString(), Kind: kind, Target: target, Name: clip(name), Reason: clip(logx.Redact(reason)), OK: ok, At: time.Now().UTC()}
	if kind == "signin.expired" {
		e.Key = "signin.expired:" + target
	}
	log := &d.data.Events
	if d.data.Account != nil {
		log.Queue = append(log.Queue, e)
		log.Queue = log.Queue[max(0, len(log.Queue)-eventsQueue):]
	} else {
		cut := time.Now().Add(-eventsKept)
		log.Local = slices.DeleteFunc(append(log.Local, e), func(x api.Event) bool { return x.At.Before(cut) })
	}
	if err := d.save(); err != nil {
		d.log.Error("save store", "err", err)
	}
	go d.flushEvents()
	d.publishEvents()
}

func (d *Daemon) publishEvents() {
	if d.srv != nil {
		d.srv.Publish(ipc.Event{Event: api.EEvents, Data: []byte("{}")})
	}
}

// entries stay queued until the server takes them, so nothing logged offline is lost
func (d *Daemon) flushEvents() {
	d.flushMu.Lock()
	defer d.flushMu.Unlock()
	for {
		d.mu.Lock()
		acct, q := d.data.Account, d.data.Events.Queue
		batch := slices.Clone(q[:min(len(q), eventsBatch)])
		d.mu.Unlock()
		if acct == nil || acct.Pending || len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		err := d.client(acct).PostEvents(ctx, batch)
		cancel()
		// a batch the server refuses as bad would block every later entry, so it is dropped
		var se *dsync.StatusError
		bad := errors.As(err, &se) && (se.Status == 400 || se.Status == 413 || se.Status == 422)
		if err != nil && !bad {
			d.log.Warn("send activity", "err", err)
			return
		}
		if err != nil {
			d.log.Warn("dropped activity the server refused", "entries", len(batch), "err", err)
		}
		sent := map[string]bool{}
		for _, e := range batch {
			sent[e.ID] = true
		}
		d.mu.Lock()
		d.data.Events.Queue = slices.DeleteFunc(d.data.Events.Queue, func(e api.Event) bool { return sent[e.ID] })
		err = d.save()
		d.mu.Unlock()
		if err != nil {
			d.log.Error("save store", "err", err)
			return
		}
	}
}

func (d *Daemon) events(ctx context.Context) (api.Events, error) {
	d.mu.Lock()
	acct := d.data.Account
	items := slices.Concat(d.data.Events.Queue, d.data.Events.Local)
	seen := d.data.Events.SeenAt
	d.mu.Unlock()
	if acct != nil && !acct.Pending {
		list, err := d.client(acct).Events(ctx, eventsShown)
		// offline, the log still shows what this device recorded
		if !dsync.Offline(err) {
			if err = d.answer(acct, err); err != nil {
				return api.Events{}, err
			}
		}
		items = append(items, list...)
	}
	slices.SortStableFunc(items, func(a, b api.Event) int { return cmp.Compare(b.At.UnixNano(), a.At.UnixNano()) })
	out := api.Events{Items: items[:min(len(items), eventsShown)]}
	for _, e := range out.Items {
		if !e.OK && e.At.After(seen) {
			out.Unseen++
		}
	}
	if len(out.Items) > 0 {
		d.mu.Lock()
		d.newestEvent = out.Items[0].At
		d.mu.Unlock()
	}
	return out, nil
}

func (d *Daemon) eventsSeen() error {
	d.mu.Lock()
	// a clock behind the server's would leave newer entries unseen forever
	d.data.Events.SeenAt = maxTime(time.Now().UTC(), d.newestEvent)
	err := d.save()
	d.mu.Unlock()
	d.publishEvents()
	return err
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
