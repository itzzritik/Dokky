package daemon

import (
	"sync"
	"time"
)

const idleAfter = 10 * time.Minute

type Idle struct {
	mu         sync.Mutex
	seen       bool
	uploads    int
	totalBytes int64
	quietSince time.Time
}

func (i *Idle) Observe(now time.Time, uploads int, totalBytes int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.seen || uploads > 0 || i.uploads > 0 || totalBytes != i.totalBytes {
		i.quietSince = now
	}
	i.seen, i.uploads, i.totalBytes = true, uploads, totalBytes
}

func (i *Idle) IsIdle(now time.Time) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.seen && i.uploads == 0 && now.Sub(i.quietSince) >= idleAfter
}

func (i *Idle) Unknown() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.seen = false
}
