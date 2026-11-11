package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

// what the system prompt says when the vault is already open
const sessionReason = "Unlock dokky"

// what works with no session: the lock screen, read-only views that hand out no secret, the launch handshake and
// the daemon's own work on request, such as installing the newest release, which auto update does anyway. anything
// else changes something or hands out a secret, so a method added later needs a session until it is listed here
var withoutSession = []string{
	"status", api.MState, api.MActivity, api.MEvents, api.MEventsSeen, "oauth.status", "providers", api.MCatalog,
	api.MSyncKick, "sync.now", "update.check", "update.install", "update.restart", "daemon.restart",
	api.MSessionOpen, api.MSessionClose, api.MVaultUnlock, api.MVaultRecover,
}

// first run has no vault, so no session to give: the welcome choice, signing in and creating or joining the
// master password work until a vault exists, then they need one like everything else
var beforeVault = []string{"settings.set", "login.start", "login.poll", "login.resolve", "login.cancel", api.MVaultSetup, api.MVaultJoin, api.MVaultJoinRecover}

var (
	errLockedMeanwhile = &codedError{api.ErrCancelled, "The screen locked before dokky unlocked. Try again."}
	errSessionRequired = &codedError{api.ErrSessionRequired, "Open dokky and unlock it to change this."}
	errVaultFirst      = &codedError{api.ErrSessionRequired, "Open dokky and create a master password to change this."}
)

// sessionGate refuses h unless a session is live, or m works without one
func sessionGate(k *keeper, m string, h ipc.Handler) ipc.Handler {
	if slices.Contains(withoutSession, m) {
		return h
	}
	first := slices.Contains(beforeVault, m)
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		if err := k.needSession(first); err != nil {
			return nil, err
		}
		return h(ctx, raw)
	}
}

func (k *keeper) needSession(beforeVault bool) error {
	switch {
	case k.live():
		return nil
	case k.state() != api.VaultSetup:
		return errSessionRequired
	case beforeVault:
		return nil
	}
	return errVaultFirst
}

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
