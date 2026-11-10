// Package lockwatch tells the daemon when the screen locks or the computer is about to sleep, so the control
// session ends when the person walks away
package lockwatch

import (
	"context"
	"sync"
	"time"
)

const (
	// a lid close fires both a lock and a sleep
	dedupeFor = time.Second
	// how often the sleep check looks, and how far the wall clock may run ahead of the monotonic one before it
	// counts as a sleep
	sleepEvery = 10 * time.Second
	sleepGap   = 30 * time.Second
	maxBackoff = time.Minute
)

// Watch calls onLock each time the screen locks or the computer sleeps, until ctx ends
func Watch(ctx context.Context, onLock func()) {
	once := dedupe(onLock, time.Now)
	var wg sync.WaitGroup
	wg.Go(func() { watchOS(ctx, once) })
	wg.Go(func() { watchSleep(ctx, once) })
	wg.Wait()
}

// dedupe drops a call within a second of the last one
func dedupe(fn func(), now func() time.Time) func() {
	var mu sync.Mutex
	var last time.Time
	return func() {
		mu.Lock()
		t := now()
		dup := !last.IsZero() && t.Sub(last) < dedupeFor
		if !dup {
			last = t
		}
		mu.Unlock()
		if !dup {
			fn()
		}
	}
}

// the monotonic clock stops while macOS and Linux sleep and the wall clock does not, so a gap between them
// catches a sleep whose event never came. Windows counts sleep in both, and has its own suspend event
func watchSleep(ctx context.Context, onSleep func()) {
	tick := time.NewTicker(sleepEvery)
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			now := time.Now()
			if slept(now.Round(0).Sub(last.Round(0)), now.Sub(last)) {
				onSleep()
			}
			last = now
		}
	}
}

func slept(wall, mono time.Duration) bool { return wall-mono > sleepGap }

// keep runs a watcher again whenever it stops, waiting longer each time one stops soon after it started
func keep(ctx context.Context, run func(context.Context)) {
	delay := time.Second
	for {
		started := time.Now()
		run(ctx)
		if time.Since(started) >= maxBackoff {
			delay = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(2*delay, maxBackoff)
	}
}
