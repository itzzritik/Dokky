package accounting

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/rclone/rclone/fs"
)

// inProgress holds a synchronized map of in progress transfers
type inProgress struct {
	mu sync.Mutex
	m  map[string]*Account
	// dokky: other live accounts of the same name, so one ending doesn't hide the rest
	more map[string][]*Account
}

// newInProgress makes a new inProgress object
func newInProgress(ctx context.Context) *inProgress {
	ci := fs.GetConfig(ctx)
	return &inProgress{
		m:    make(map[string]*Account, ci.Transfers),
		more: map[string][]*Account{},
	}
}

// set marks the name as in progress
func (ip *inProgress) set(name string, acc *Account) {
	ip.mu.Lock()
	defer ip.mu.Unlock()
	if cur := ip.m[name]; cur != nil && cur != acc && !ended(cur) {
		ip.more[name] = append(ip.more[name], cur)
	}
	ip.m[name] = acc
}

// dokky: Done closes exit before it clears, so this tells which account ended without taking its lock
func ended(acc *Account) bool {
	select {
	case <-acc.exit:
		return true
	default:
		return false
	}
}

// clear marks the name as no longer in progress
func (ip *inProgress) clear(name string) {
	ip.mu.Lock()
	defer ip.mu.Unlock()
	rest := ip.more[name]
	if len(rest) == 0 {
		delete(ip.m, name)
		return
	}
	if cur := ip.m[name]; cur == nil || ended(cur) {
		ip.m[name], rest = rest[len(rest)-1], rest[:len(rest)-1]
	} else if i := slices.IndexFunc(rest, ended); i >= 0 {
		rest = slices.Delete(rest, i, i+1)
	}
	if len(rest) == 0 {
		delete(ip.more, name)
	} else {
		ip.more[name] = rest
	}
}

// get gets the account for name, of nil if not found
func (ip *inProgress) get(name string) *Account {
	ip.mu.Lock()
	defer ip.mu.Unlock()
	return ip.m[name]
}

// merge adds items from another inProgress
func (ip *inProgress) merge(m *inProgress) {
	ip.mu.Lock()
	defer ip.mu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	maps.Copy(ip.m, m.m)
	for name, rest := range m.more {
		ip.more[name] = append(ip.more[name], rest...)
	}
}
