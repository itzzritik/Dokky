package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
)

// what a removed device may still do while it leaves
var whileRemoved = []string{"status", api.MState, api.MActivity, api.MEvents, api.MEventsSeen, "settings.get", "logout", "sync.now", api.MSyncKick,
	"update.check", "update", "update.install", "update.restart", "daemon.restart"}

func (d *Daemon) removed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.data.Removal != nil
}

func (d *Daemon) refuseRemoved(h ipc.Handler) ipc.Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		if d.removed() {
			return nil, &codedError{"removed", "This device was removed from your dokky account"}
		}
		return h(ctx, raw)
	}
}

// an answer for an account that was replaced since the call started is ignored
func (d *Daemon) removedBy(acct *store.Account, err error) {
	d.mu.Lock()
	cur := d.data.Account
	d.mu.Unlock()
	if cur != nil && cur.Token == acct.Token {
		d.startLeaving(dsync.RevokedMode(err))
	}
}

// the server removed this device; erase wins over remove. Safe with opMu held.
func (d *Daemon) startLeaving(mode string) {
	d.mu.Lock()
	r := d.data.Removal
	switch {
	case r == nil:
		r = &store.Removal{Mode: mode, At: time.Now()}
	case mode == "erase" && r.Mode != "erase":
		cp := *r
		cp.Mode = mode
		r = &cp
	case d.departing:
		d.mu.Unlock()
		return
	}
	d.data.Removal = r
	mounts := slices.Clone(d.data.Mounts)
	start := !d.departing
	d.departing = true
	clear(d.unsent)
	d.mu.Unlock()
	if r.Mode == "remove" && !r.Erased {
		for _, m := range mounts {
			// a drive that isn't mounted mounts again to send what it still holds
			if n := d.unsentOf(m); n > 0 {
				d.mu.Lock()
				lm := d.data.Local[m.ID]
				lm.Mounted = true
				d.data.Local[m.ID] = lm
				d.mu.Unlock()
			}
		}
	}
	d.mu.Lock()
	if err := d.save(); err != nil {
		d.log.Error("save store", "err", err)
	}
	d.mu.Unlock()
	d.log.Warn("this device was removed from the account", "mode", r.Mode)
	if start {
		go d.leave()
	}
	d.publish()
}

func (d *Daemon) resumeLeaving() {
	d.mu.Lock()
	r := d.data.Removal
	d.mu.Unlock()
	if r != nil {
		d.startLeaving(r.Mode)
	}
}

// real transfers finish, stuck ones are dropped, then the device erases, reports and forgets
func (d *Daemon) leave() {
	defer func() {
		d.mu.Lock()
		d.departing = false
		d.mu.Unlock()
	}()
	for {
		d.mu.Lock()
		r := d.data.Removal
		d.mu.Unlock()
		if r == nil {
			return
		}
		if !r.Erased {
			if why := d.busyLeaving(r.Mode); why != "" {
				d.setLeaving(why, 0)
				time.Sleep(drainEvery)
				continue
			}
			d.opMu.Lock()
			if !d.removed() {
				d.opMu.Unlock()
				return
			}
			// counted again from disk, so a file written in the last few seconds is never erased
			d.mu.Lock()
			clear(d.unsent)
			d.mu.Unlock()
			if why := d.busyLeaving(r.Mode); why != "" {
				d.opMu.Unlock()
				time.Sleep(drainEvery)
				continue
			}
			err := d.eraseDevice()
			d.opMu.Unlock()
			if err != nil {
				d.log.Warn("erase this device", "err", err)
				time.Sleep(drainRetry)
				continue
			}
		}
		// tokens refreshed while draining reach the server before it stops taking them
		if err := d.flushTokens(); err != nil {
			d.log.Warn("send refreshed tokens", "err", err)
			time.Sleep(drainRetry)
			continue
		}
		if err := d.reportErased(); err != nil {
			d.log.Warn("report this device erased", "err", err)
			time.Sleep(drainRetry)
			continue
		}
		d.opMu.Lock()
		if !d.removed() {
			d.opMu.Unlock()
			return
		}
		err := d.forget()
		d.opMu.Unlock()
		if err != nil {
			d.log.Error("delete store", "err", err)
		}
		d.log.Info("this device was removed and erased")
		return
	}
}

// the first drive still moving data, or "" when nothing needs waiting for
func (d *Daemon) busyLeaving(mode string) string {
	if mode != "remove" {
		return ""
	}
	d.mu.Lock()
	mounts := slices.Clone(d.data.Mounts)
	d.mu.Unlock()
	// every drive is looked at each time, so no drive's timers go stale behind a busy one
	now, why := time.Now(), ""
	for _, m := range mounts {
		d.mu.Lock()
		st := d.state(m.ID)
		down := d.data.Local[m.ID].Mounted && st.state != stateMounted
		d.mu.Unlock()
		// a drive whose storage is gone while dokky answers is stuck, never waited for
		if d.failingFor("mount:"+m.ID, down && d.storageLost(m), now) >= stuckAfter {
			continue
		}
		if u := d.usageOf(m.ID, true); u.State == usageBusy && why == "" {
			why = m.Name + ": " + u.Reason
		}
	}
	return why
}

const reachEvery = 30 * time.Second

// the storage doesn't answer but dokky does: this device is online and those files can't go anywhere
func (d *Daemon) storageLost(m store.Mount) bool {
	d.mu.Lock()
	c, ok := d.lost[m.ID]
	i := slices.IndexFunc(d.data.Services, func(s store.Service) bool { return s.ID == m.ServiceID })
	var svc store.Service
	if i >= 0 {
		svc = d.data.Services[i]
	}
	d.mu.Unlock()
	if ok && time.Since(c.at) < reachEvery {
		return c.lost
	}
	lost := (i < 0 || d.eng.Reachable(svc, m.RemotePath) != nil) && d.dokkyReachable()
	d.mu.Lock()
	d.lost[m.ID] = lostCheck{lost, time.Now()}
	d.mu.Unlock()
	return lost
}

func (d *Daemon) dokkyReachable() bool {
	d.mu.Lock()
	acct, c := d.data.Account, d.reached
	d.mu.Unlock()
	if acct == nil {
		return false
	}
	if time.Since(c.at) < reachEvery {
		return !c.lost
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	_, err := client(acct).Me(ctx)
	up := !dsync.Offline(err)
	d.mu.Lock()
	d.reached = lostCheck{!up, time.Now()}
	d.mu.Unlock()
	return up
}

func (d *Daemon) lastWrite(id string) time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.unsent[id].changed
}

func (d *Daemon) flushTokens() error {
	d.mu.Lock()
	acct := d.data.Account
	d.mu.Unlock()
	if acct == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	err := d.loop.Flush(ctx, client(acct))
	// a server that refuses them will never take them
	if errors.Is(err, dsync.ErrRevoked) || errors.Is(err, dsync.ErrUnauthorized) {
		return nil
	}
	return err
}

// erase wins over remove
func stronger(a, b string) string {
	if a == "erase" || b == "erase" {
		return "erase"
	}
	return "remove"
}

type lostCheck struct {
	lost bool
	at   time.Time
}

func (d *Daemon) setLeaving(waiting string, progress float64) {
	d.mu.Lock()
	d.leaveWaiting, d.leaveProgress = waiting, progress
	d.mu.Unlock()
	d.publish()
}

// caller holds opMu; saved sign-ins go first, then mounts, folders, cache and local state
func (d *Daemon) eraseDevice() error {
	d.setLeaving("", 0)
	d.mu.Lock()
	services := d.data.Services
	cleared := make([]store.Service, len(services))
	for i, s := range services {
		s.Secrets = nil
		cleared[i] = s
	}
	d.data.Services = cleared
	err := d.save()
	mounts := slices.Clone(d.data.Mounts)
	d.mu.Unlock()
	if err != nil {
		return err
	}
	d.setLeaving("", 0.1)
	for i, m := range mounts {
		if err := d.purge(m); err != nil {
			return err
		}
		d.setLeaving("", 0.1+0.8*float64(i+1)/float64(len(mounts)))
	}
	for _, s := range services {
		d.eng.DeleteService(s.ID)
	}
	if err := d.clearCache(); err != nil {
		return err
	}
	d.mu.Lock()
	r := *d.data.Removal
	r.Erased = true
	left := store.Fresh()
	left.Account, left.Removal = d.data.Account, &r
	d.data = left
	d.setSecrets()
	err = d.save()
	d.mu.Unlock()
	d.setLeaving("", 1)
	return err
}

func (d *Daemon) reportErased() error {
	d.mu.Lock()
	acct := d.data.Account
	d.mu.Unlock()
	if acct == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	err := client(acct).Erased(ctx)
	var se *dsync.StatusError
	switch {
	case err == nil, errors.Is(err, dsync.ErrRevoked), errors.Is(err, dsync.ErrUnauthorized), errors.Is(err, dsync.ErrNotFound):
		return nil
	case errors.As(err, &se) && se.Status < 500:
		// the server never knew this removal (a pending login); nothing is left to tell it
		return nil
	}
	return err
}
