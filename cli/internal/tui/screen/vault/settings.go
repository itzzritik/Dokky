package vault

import (
	"context"
	"slices"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

var unlockModes = []string{api.UnlockAuto, api.UnlockSystem, api.UnlockPassword}

// how dokky opens the vault when it starts, as the Security row shows it
func UnlockLabel(st *core.State) string {
	switch st.Snap.Vault.Unlock {
	case api.UnlockSystem:
		return systemChoice(st.Snap.Vault)
	case api.UnlockPassword:
		return "Master password"
	}
	return "Automatically"
}

func DaysLabel(n int) string {
	if n <= 0 {
		return "Never"
	}
	return strconv.Itoa(n) + " days"
}

func IdleLabel(n int) string {
	switch {
	case n <= 0:
		return "Never"
	case n%60 == 0:
		return core.Count(n/60, "hour")
	}
	return strconv.Itoa(n) + " min"
}

// the Security heading's note: only drives in the account still waiting to be encrypted
func Status(st *core.State) string {
	if n := st.Snap.Vault.Legacy; n > 0 && st.Snap.Account.LoggedIn {
		return ui.L(nil).T(core.Count(n, "drive")+" still finishing encryption", ui.P().Buoy).String()
	}
	return ""
}

func PickUnlock(st *core.State) tea.Cmd {
	v := st.Snap.Vault
	sys := choice{label: systemChoice(v), note: "Asks when dokky starts"}
	if v.System == "" {
		sys.off = "Not available on this computer"
	}
	opts := []choice{
		{label: "Automatically", note: "No prompt, drives mount at login"},
		sys,
		{label: "Master password", note: "Typed each time dokky starts"},
	}
	return openPick(st, &pick{title: "Unlock at startup", sub: "How dokky opens your drives when it starts.", opts: opts,
		cur: max(0, slices.Index(unlockModes, v.Unlock)),
		save: func(st *core.State, i int) tea.Cmd {
			return save(st, "To change how dokky unlocks.", api.VaultSettingsParams{Unlock: &unlockModes[i]})
		}})
}

func PickDays(st *core.State) tea.Cmd {
	v := st.Snap.Vault
	var opts []choice
	for _, n := range api.PasswordDayChoices {
		opts = append(opts, choice{label: DaysLabel(n)})
	}
	return openPick(st, &pick{title: "Ask for master password every", sub: "In between, " + systemName(v) + " is enough.", opts: opts,
		cur: max(0, slices.Index(api.PasswordDayChoices, v.PasswordDays)),
		save: func(st *core.State, i int) tea.Cmd {
			n := api.PasswordDayChoices[i]
			return save(st, "To change how often dokky asks.", api.VaultSettingsParams{PasswordDays: &n})
		}})
}

func PickIdle(st *core.State) tea.Cmd {
	var opts []choice
	for _, n := range api.IdleLockChoices {
		opts = append(opts, choice{label: IdleLabel(n)})
	}
	return openPick(st, &pick{title: "Lock dokky after", sub: "When you're away. Drives keep working.", opts: opts,
		cur: max(0, slices.Index(api.IdleLockChoices, st.Snap.Vault.IdleLockMinutes)),
		save: func(st *core.State, i int) tea.Cmd {
			n := api.IdleLockChoices[i]
			return save(st, "To change when dokky locks.", api.VaultSettingsParams{IdleLockMinutes: &n})
		}})
}

func openPick(st *core.State, pk *pick) tea.Cmd {
	pk.sel = pk.cur
	return core.Open(newFlow(st, pk, true, exitClose))
}

// a grant first, then the change
func save(st *core.State, reason string, p api.VaultSettingsParams) tea.Cmd {
	return Authorize(st, reason, func(st *core.State, grant string) tea.Cmd {
		p.Grant = grant
		c, v := st.C, st.Snap.Vault
		return func() tea.Msg {
			if err := c.VaultSettings(context.Background(), p); err != nil {
				return core.Toast{Text: explain(err, v, time.Now()), Bad: true}
			}
			return core.Toast{Text: "Saved"}
		}
	})
}

// LockWindow asks the app to lock dokky like a lock screen; drives and syncing carry on
type LockWindow struct{}

func LockNow(*core.State) tea.Cmd { return core.Send(LockWindow{}) }
