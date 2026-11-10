package vault

import (
	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

const encrypted = "Master password set. dokky is end-to-end encrypted."

// Unlock is the lock screen: system auth or the master password opens a control session, and unlocks the daemon
// when it is locked; force asks again even while a session is live, after the view was locked
func Unlock(st *core.State, force bool) *Flow {
	f := newFlow(st, newUnlock(force), false, exitQuit)
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

func ChangePassword(st *core.State) tea.Cmd {
	return core.Open(newFlow(st, newChange(), true, exitClose))
}

func NewRecoveryKey(st *core.State) tea.Cmd {
	return core.Open(newFlow(st, newAskKey(), true, exitClose))
}
