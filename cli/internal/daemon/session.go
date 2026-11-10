package daemon

import (
	"context"
	"encoding/json"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

// what the system prompt says when the vault is already open
const sessionReason = "Unlock dokky"

var errLockedMeanwhile = &codedError{api.ErrCancelled, "The screen locked before dokky unlocked. Try again."}

// the monotonic clock stops while the computer sleeps, so wall time also ends a session whose sleep event was
// missed
func sessionLive(until, now time.Time) bool {
	return !until.IsZero() && now.Before(until) && now.Round(0).Before(until.Round(0))
}

// live says a control session is open, so changes are allowed
func (k *keeper) live() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return sessionLive(k.sessionUntil, k.now())
}

// generation is read before proving the person, so a session that ended meanwhile is not started again
func (k *keeper) generation() uint64 {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.sessionGen
}

// startSession makes the session live for api.SessionFor, unless it ended since gen was read
func (k *keeper) startSession(gen uint64) bool {
	k.mu.Lock()
	ok := gen == k.sessionGen
	if ok {
		k.sessionUntil = k.now().Add(api.SessionFor)
	}
	k.mu.Unlock()
	if ok {
		k.publish()
	}
	return ok
}

// endSession ends the control session; drives carry on. why goes to the log
func (k *keeper) endSession(why string) {
	k.mu.Lock()
	was := k.endLocked()
	k.mu.Unlock()
	if was {
		k.log.Info("the control session ended", "why", why)
		k.publish()
	}
}

// caller holds mu; a lock or an erase ends the session this way, and the next phase publishes it
func (k *keeper) endLocked() bool {
	was := sessionLive(k.sessionUntil, k.now())
	k.sessionUntil = time.Time{}
	k.sessionGen++
	return was
}

// onPublish hands the keeper the running phase's publish, nil once the phase ends
func (k *keeper) onPublish(fn func()) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.published = fn
}

// the app sees a session start or end at once rather than on its next poll
func (k *keeper) publish() {
	k.mu.Lock()
	fn := k.published
	k.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// openSession reuses a live session unless p.Force, else proves the person and starts one. the account key comes
// back only when the person was proved, for the locked phase to open the vault with; the caller owns it
func (k *keeper) openSession(ctx context.Context, p api.SessionParams, reason string) ([]byte, error) {
	gen := k.generation()
	if !p.Force && k.live() {
		return nil, nil
	}
	defer k.holdFor(p.Method)()
	ak, err := k.prove(ctx, api.UnlockParams{Method: p.Method, Password: p.Password, Remote: p.Remote}, reason)
	if err != nil {
		return nil, err
	}
	if !k.startSession(gen) {
		vault.Zero(ak)
		return nil, errLockedMeanwhile
	}
	return ak, nil
}

// proving starts a session once h succeeds: it checked the master password or recovery key, so the app goes
// straight in rather than asking again
func proving(k *keeper, h ipc.Handler) ipc.Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		gen := k.generation()
		out, err := h(ctx, raw)
		if err == nil {
			k.startSession(gen)
		}
		return out, err
	}
}

// sessionOpen is session.open while the vault is open; vault.unlock is the same with Force
func (d *Daemon) sessionOpen(ctx context.Context, p api.SessionParams) error {
	if d.vk.state() == api.VaultSetup {
		return errNoVault
	}
	ak, err := d.vk.openSession(ctx, p, sessionReason)
	vault.Zero(ak)
	return err
}
