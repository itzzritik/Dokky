package vault

import (
	"context"
	"slices"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
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

// the Security heading's note: encrypted, and what still waits for it in the account
func Status(st *core.State) string {
	p := ui.P()
	l := ui.L(nil).B(ui.G.Check, p.Kelp).Sp(1).T("End-to-end encrypted", p.Muted)
	if n := st.Snap.Vault.Legacy; n > 0 && st.Snap.Account.LoggedIn {
		l.T(" · ", p.Faint).T(core.Count(n, "drive")+" still finishing encryption", p.Buoy)
	}
	return l.String()
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
	return openPick(st, &pick{title: "Lock window after", sub: "Hides this window when you're away. Drives keep working.", opts: opts,
		cur: max(0, slices.Index(api.IdleLockChoices, st.Snap.Vault.IdleLockMinutes)),
		save: func(st *core.State, i int) tea.Cmd {
			n := api.IdleLockChoices[i]
			return save(st, "To change when this window locks.", api.VaultSettingsParams{IdleLockMinutes: &n})
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

// the drives unmount and dokky waits for the master password or Touch ID
func LockNow(*core.State) tea.Cmd {
	return confirm.Open(&confirm.Confirm{
		Question: "Lock dokky now?",
		Line:     "Your drives unmount until you unlock dokky again.",
		Do:       "Lock",
		Run: func(st *core.State) tea.Cmd {
			c, v := st.C, st.Snap.Vault
			return func() tea.Msg {
				if err := c.VaultLock(context.Background()); err != nil {
					return core.Toast{Text: explain(err, v, time.Now()), Bad: true}
				}
				return nil
			}
		},
	})
}
