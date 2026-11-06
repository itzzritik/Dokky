package daemon

import (
	"context"
	"net"
	"slices"
	"strings"
	"time"
)

const (
	syncEvery  = 15 * time.Minute
	syncCheck  = 30 * time.Second
	sleptAfter = 2 * time.Minute
)

var syncRetries = []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute}

func retryAfter(fails int) time.Duration {
	if fails <= len(syncRetries) {
		return syncRetries[fails-1]
	}
	return syncEvery
}

func (d *Daemon) kick() {
	select {
	case d.kicks <- struct{}{}:
	default:
	}
}

// wall time only: the monotonic clock stops while a Mac sleeps
func wall(t time.Time) time.Time { return t.Round(0) }

func netAddrs() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	slices.Sort(out)
	return strings.Join(out, ",")
}

// the only background puller; a kick during a pull runs one more after it
func (d *Daemon) syncLoop(ctx context.Context) {
	check := time.NewTicker(syncCheck)
	defer check.Stop()
	var okAt, retryAt time.Time
	fails, last, addrs := 0, wall(time.Now()), netAddrs()
	run := func() {
		if err := d.syncNow(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			fails++
			retryAt = wall(time.Now()).Add(retryAfter(fails))
			d.log.Warn("sync failed", "err", err, "fails", fails, "retry in", retryAfter(fails))
			return
		}
		fails, okAt = 0, wall(time.Now())
	}
	d.reportDue.Store(true)
	run()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.kicks:
			run()
		case <-check.C:
			now := wall(time.Now())
			// a jump either way, from sleep or a clock fix, leaves the timers wrong, so sync now
			slept := now.Sub(last) > sleptAfter || last.Sub(now) > time.Minute
			last = now
			a := netAddrs()
			moved := a != addrs
			addrs = a
			switch {
			case slept:
				d.log.Info("syncing after sleep")
				run()
			case fails == 0 && now.Sub(okAt) >= syncEvery:
				run()
			case fails > 0 && (!now.Before(retryAt) || moved):
				run()
			}
		}
	}
}
