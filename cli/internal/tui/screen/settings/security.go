package settings

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/vault"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// the master password's settings, once there is one; the open session covers each change
func (s *Settings) security(st *core.State) []row {
	v := st.Snap.Vault
	if v.State != api.VaultUnlocked {
		return nil
	}
	days := make([]string, len(api.PasswordDayChoices))
	for i, n := range api.PasswordDayChoices {
		days[i] = vault.DaysLabel(n)
	}
	rows := []row{
		s.choice("Ask for master password every", ui.G.Icon.Clock, days, slices.Index(api.PasswordDayChoices, v.PasswordDays),
			func(st *core.State, i int) tea.Cmd { return vault.SetDays(st, api.PasswordDayChoices[i]) }),
		{label: "Change master password", icon: ui.G.Icon.Key, kind: action, run: vault.ChangePassword},
		{label: "New recovery key", icon: ui.G.Icon.Recovery, kind: action, run: vault.NewRecoveryKey},
	}
	// sync waits until this device has the account's key
	if v.Need == api.NeedJoin && !st.Snap.Account.Pending {
		rows = append([]row{{label: "Sync is paused", icon: ui.G.Icon.Sync, value: "enter the account's password", kind: action, tone: ui.P().Buoy, run: vault.OpenJoin}}, rows...)
	}
	rows[0].group, rows[0].note = "Security", vault.Status(st)
	return rows
}
