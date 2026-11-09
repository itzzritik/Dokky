package vault

import (
	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

const encrypted = "Master password set. dokky is end-to-end encrypted."

// the daemon is locked: drives wait until this unlocks
func Unlock(st *core.State) *Flow {
	return newFlow(st, newUnlock(atStart, "Unlock dokky", "Your drives mount once dokky is unlocked."), false, exitQuit)
}

// covers dokky until the person proves it is them; the daemon and the drives carry on
func WindowLock(st *core.State) *Flow {
	f := newFlow(st, newUnlock(onWindow, "Unlock dokky", "dokky locked this window. Your drives keep working."), false, exitQuit)
	f.done = func(*core.State) tea.Cmd { return core.Send(Released{}) }
	return f
}

const (
	createTitle = "Create a master password"
	createSub   = "Encrypts your drive logins. Only you know it."
)

// every dokky needs a master password before its dashboard: after the first-run choice, or once for an older install
func Setup(st *core.State) *Flow {
	return newFlow(st, newCreate(createTitle, createSub, encrypted), false, exitQuit)
}

// a signed-in dokky whose account set its master password on another device
func Join(st *core.State) *Flow {
	return newFlow(st, newJoin("Master password added. dokky is end-to-end encrypted."), false, exitQuit)
}

// the step a sign in waits for (need): a master password for an account without one, or the one it has; resume finishes the sign in
func SignIn(st *core.State, need string, resume func(*core.State) tea.Cmd) *Flow {
	var first form = newCreate(createTitle, createSub, "Master password set")
	if need == api.NeedJoin {
		first = newJoin("")
	}
	f := newFlow(st, first, true, exitStay)
	f.signin, f.done = true, resume
	return f
}

// sync waits because the account's key is not this device's: type the account's password to join it
func OpenJoin(st *core.State) tea.Cmd {
	f := newFlow(st, newJoin("Sync is back on"), true, exitClose)
	return core.Open(f)
}

// Touch ID or Windows Hello when offered, which starts at once, else the master password; then spends the grant
func Authorize(st *core.State, reason string, then func(*core.State, string) tea.Cmd) tea.Cmd {
	u := newUnlock(forGrant, "Confirm it's you", reason)
	u.then = then
	f := newFlow(st, u, true, exitClose)
	if offered(st) {
		return tea.Sequence(core.Open(f), u.ask(st, api.AuthSystem))
	}
	return core.Open(f)
}

func ChangePassword(st *core.State) tea.Cmd {
	return core.Open(newFlow(st, newChange(), true, exitClose))
}

func NewRecoveryKey(st *core.State) tea.Cmd {
	return core.Open(newFlow(st, newAskKey(), true, exitClose))
}
