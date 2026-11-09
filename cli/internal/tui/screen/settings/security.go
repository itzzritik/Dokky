package settings

import (
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/vault"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// the master password's settings, once there is one; each change asks for Touch ID or the password first
func (s *Settings) security(st *core.State) []row {
	v := st.Snap.Vault
	if v.State != api.VaultUnlocked {
		return nil
	}
	rows := []row{
		{label: "Unlock at startup", value: vault.UnlockLabel(st), kind: action, run: vault.PickUnlock},
		{label: "Ask for master password every", value: vault.DaysLabel(v.PasswordDays), kind: action, run: vault.PickDays},
		{label: "Lock window after", value: vault.IdleLabel(v.IdleLockMinutes), kind: action, run: vault.PickIdle},
		{label: "Change master password", kind: action, run: vault.ChangePassword},
		{label: "New recovery key", kind: action, run: vault.NewRecoveryKey},
		{label: "Lock dokky now", value: "unmounts drives", kind: action, tone: ui.P().Faint, run: vault.LockNow},
	}
	// sync waits until this device has the account's key
	if v.Need == api.NeedJoin && !st.Snap.Account.Pending {
		rows = append([]row{{label: "Sync is paused", value: "enter the account's password", kind: action, tone: ui.P().Buoy, run: vault.OpenJoin}}, rows...)
	}
	rows[0].group, rows[0].note = "Security", vault.Status(st)
	return rows
}
