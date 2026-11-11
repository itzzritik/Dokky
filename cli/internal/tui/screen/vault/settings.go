package vault

import (
	"context"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

func DaysLabel(n int) string { return strconv.Itoa(n) + " days" }

// the Security heading's note: only drives in the account still waiting to be encrypted
func Status(st *core.State) string {
	if n := st.Snap.Vault.Legacy; n > 0 && st.Snap.Account.LoggedIn {
		return ui.L(nil).T(core.Count(n, "drive")+" still finishing encryption", ui.P().Buoy).String()
	}
	return ""
}

// SetDays saves how often the master password is asked for; the open session covers the change
func SetDays(st *core.State, n int) tea.Cmd {
	c, v := st.C, st.Snap.Vault
	return func() tea.Msg {
		if err := c.VaultSettings(context.Background(), api.VaultSettingsParams{PasswordDays: &n}); err != nil {
			return core.Toast{Text: explain(err, v, time.Now()), Bad: true}
		}
		return core.Toast{Text: "Saved"}
	}
}

// LockWindow asks the app to lock dokky like a lock screen; drives and syncing carry on
type LockWindow struct{}
