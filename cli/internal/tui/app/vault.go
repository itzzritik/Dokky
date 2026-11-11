package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/gate"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/vault"
)

// what covers the tabs, in the gate's place
type coverKind int

const (
	noCover     coverKind = iota
	gateCover             // the first-run choice
	unlockCover           // locked: the daemon's vault, no control session, or the view
	createCover           // no master password yet
	joinCover             // the account has one from another device
)

// the vault before the first-run gate, and the gate before setup; a pending sign in shows its own step in a window
func coverFor(st *core.State) coverKind {
	s := st.Snap
	switch {
	case !st.Connected || s.Removed != nil:
		return noCover
	case s.Vault.State == api.VaultLocked:
		return unlockCover
	// changes need a control session, which only system auth or the password opens, as in Forged
	case s.Vault.State == api.VaultUnlocked && (st.Locked || !s.Vault.Session):
		return unlockCover
	case wantGate(st):
		return gateCover
	case s.Vault.State != api.VaultSetup || s.Account.Pending:
		return noCover
	case s.Vault.Need == api.NeedJoin:
		return joinCover
	}
	return createCover
}

// sign in waits on the master password step
func signInStep(st *core.State) bool {
	s := st.Snap
	return st.Connected && s.Account.Pending && s.Vault.Need != "" && s.Vault.State != api.VaultLocked
}

type holder interface{ Holding() bool }

const stepSettle = 3 * time.Second

// puts up the screen coverFor wants; one mid call or showing a recovery key stays until it settles
func (a *App) route() tea.Cmd {
	if h, ok := a.gate.(holder); ok && h.Holding() {
		return nil
	}
	want := coverFor(a.st)
	if want == a.covered && (a.gate != nil) == (want != noCover) {
		return nil
	}
	a.covered = want
	var start tea.Cmd
	switch want {
	case noCover:
		a.gate = nil
	case gateCover:
		a.gate = gate.New(a.st)
	case unlockCover:
		// nothing changes behind a lock screen: every window goes, wiping what was typed
		a.windows = nil
		f := vault.Unlock(a.st, a.st.Locked)
		a.gate, start = f, f.Start(a.st)
	case createCover:
		a.gate = vault.Setup(a.st)
	case joinCover:
		a.gate = vault.Join(a.st)
	}
	return tea.Batch(start, a.nextToast(), a.armIdle())
}

// the daemon locked, or the control session ended (the screen locked, the computer slept, 4 hours passed):
// windows would only fail now, so they go after wiping what was typed
func (a *App) onVault(was api.Vault) tea.Cmd {
	v := a.st.Snap.Vault
	var cmd tea.Cmd
	lost := was.State == api.VaultUnlocked && was.Session && v.State == api.VaultUnlocked && !v.Session
	if v.State == api.VaultLocked && was.State != api.VaultLocked || lost {
		cmd = a.broadcast(core.Locked{})
		a.windows = nil
		a.st.Locked = a.st.Locked || lost
	}
	// a state from before the step finished can trail its reply; the resumed sign in asks again itself
	if signInStep(a.st) && !a.has(signInOpen) && a.st.Now.Sub(a.stepClosed) > stepSettle {
		a.windows = append(a.windows, vault.SignIn(a.st, a.st.Snap.Vault.Need, gate.Resume))
	}
	return cmd
}

// the device code window hands over to the step itself
func signInOpen(s core.Screen) bool {
	if f, ok := s.(*vault.Flow); ok {
		return f.SignInStep()
	}
	_, ok := s.(*gate.SignIn)
	return ok
}

func (a *App) has(match func(core.Screen) bool) bool {
	for _, w := range a.windows {
		if match(w) {
			return true
		}
	}
	return false
}

// locking needs a vault to answer to, and Updating stays on top while it runs
func (a *App) canLock() bool {
	return a.st.Connected && a.st.Snap.Vault.State == api.VaultUnlocked && !a.st.Locked && a.gate == nil && !a.updatingNow()
}

type sessionClosed struct{}

// Forged's lock: every window closes and what was typed is wiped, the control session ends and the lock screen
// asks for system auth again; drives and the daemon carry on
func (a *App) lockWindow() tea.Cmd {
	cmd := a.broadcast(core.Locked{})
	a.st.Locked = true
	a.disarmIdle()
	c := a.st.C
	end := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.SessionClose(ctx)
		return sessionClosed{}
	}
	return tea.Batch(cmd, end, a.route())
}

func (a *App) released() tea.Cmd {
	a.st.Locked, a.lastInput = false, a.st.Now
	return tea.Batch(a.route(), a.nextToast(), a.armIdle())
}

type idleCheck struct{ gen int }

// Forged locks after 4 minutes without a key or a click, a fixed time
const idleAfter = 4 * time.Minute

// how long the window may sit untouched, 0 while nothing is open to lock
func (a *App) idleLimit() time.Duration {
	if !a.canLock() {
		return 0
	}
	return idleAfter
}

// one timer at a time, armed again whenever the limit changes; input only moves lastInput
func (a *App) armIdle() tea.Cmd {
	limit := a.idleLimit()
	if limit == a.idleArmed {
		return nil
	}
	a.disarmIdle()
	if limit == 0 {
		return nil
	}
	a.idleArmed = limit
	gen := a.idleGen
	wait := max(time.Second, limit-a.st.Now.Sub(a.lastInput))
	return tea.Tick(wait, func(time.Time) tea.Msg { return idleCheck{gen} })
}

func (a *App) disarmIdle() {
	a.idleGen++
	a.idleArmed = 0
}

func (a *App) onIdle(m idleCheck) tea.Cmd {
	if m.gen != a.idleGen {
		return nil
	}
	limit := a.idleLimit()
	a.disarmIdle()
	if limit > 0 && a.st.Now.Sub(a.lastInput) >= limit {
		return a.lockWindow()
	}
	return a.armIdle()
}
