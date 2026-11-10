package api

import "time"

const (
	MVaultSetup          = "vault.setup"
	MVaultUnlock         = "vault.unlock"
	MVaultRecover        = "vault.recover"
	MVaultLock           = "vault.lock"
	MVaultAuthorize      = "vault.authorize"
	MVaultSettings       = "vault.settings"
	MVaultChangePassword = "vault.changePassword"
	MVaultNewRecoveryKey = "vault.newRecoveryKey"
	MVaultJoin           = "vault.join"
	MVaultJoinRecover    = "vault.joinRecover"
	MSessionOpen         = "session.open"
	MSessionClose        = "session.close"
)

// SessionFor is how long one control session lasts; drives never need one, changes do
const SessionFor = 4 * time.Hour

// vault states
const (
	VaultSetup    = "setup"    // no master password yet; drives still work on the old keychain key
	VaultLocked   = "locked"   // the key is not in memory; drives are not mounted
	VaultUnlocked = "unlocked" // normal operation
)

// what a pending sign in waits for
const (
	NeedCreate = "create" // the account has no vault: this device's new password becomes the account's
	NeedJoin   = "join"   // the account has a vault: type its password or recovery key
)

// how the daemon opened the vault when it started; it always uses the device key now
//
// Deprecated: removed with the TUI change
const (
	UnlockAuto     = "auto"     // the device key in the OS keychain, no prompt
	UnlockSystem   = "system"   // Touch ID, Windows Hello or polkit, then the device key
	UnlockPassword = "password" // the master password every time
)

// ways to prove it is the user
const (
	AuthPassword = "password"
	AuthSystem   = "system"
)

// coded errors the vault methods return
const (
	ErrLocked            = "locked"
	ErrWrongPassword     = "wrong_password"
	ErrPasswordRequired  = "password_required"
	ErrSystemUnavailable = "system_unavailable"
	ErrCancelled         = "cancelled"
	ErrTooManyAttempts   = "too_many_attempts"
	ErrWeakPassword      = "weak_password"
	ErrBadRecoveryKey    = "bad_recovery_key"
	ErrGrantRequired     = "grant_required"
	ErrSessionRequired   = "session_required"
)

var PasswordDayChoices = []int{7, 15, 30}

// Deprecated: removed with the TUI change; the app locks itself after 4 minutes
var IdleLockChoices = []int{5, 10, 30, 60, 0}

type Vault struct {
	State           string    `json:"state"`
	Need            string    `json:"need,omitempty"`
	Unlock          string    `json:"unlock"` // Deprecated: removed with the TUI change
	PasswordDays    int       `json:"passwordDays"`
	IdleLockMinutes int       `json:"idleLockMinutes"`  // Deprecated: removed with the TUI change
	PasswordDue     bool      `json:"passwordDue"`      // system auth is refused until the password is typed once, e.g. after it changed
	System          string    `json:"system,omitempty"` // "" when unavailable, else "Touch ID", "Windows Hello", "your Mac password", "your system password"
	Synced          bool      `json:"synced"`           // this device's key is the signed-in account's key
	Legacy          int       `json:"legacy"`           // services on the server still waiting to be end-to-end encrypted
	CooldownUntil   time.Time `json:"cooldownUntil,omitzero"`
	KeyID           string    `json:"keyId,omitempty"`
	Session         bool      `json:"session"` // a control session is live, so changes are allowed
}

// SessionParams opens a control session. a live one is reused unless Force, which the app sets after a lock
type SessionParams struct {
	Method   string `json:"method"`
	Password string `json:"password,omitempty"`
	Force    bool   `json:"force,omitempty"`
	Remote   bool   `json:"remote,omitempty"` // the TUI runs over SSH, so no system prompt on this screen
}

type PasswordParams struct {
	Password string `json:"password"`
}

type UnlockParams struct {
	Method   string `json:"method"`
	Password string `json:"password,omitempty"`
	Remote   bool   `json:"remote,omitempty"` // the TUI runs over SSH, so no system prompt on this screen
}

type RecoverParams struct {
	RecoveryKey string `json:"recoveryKey"`
	NewPassword string `json:"newPassword"`
}

type ChangePasswordParams struct {
	Current string `json:"current"`
	Next    string `json:"next"`
}

type VaultSettingsParams struct {
	Grant           string  `json:"grant"`
	Unlock          *string `json:"unlock,omitempty"` // Deprecated: removed with the TUI change, ignored
	PasswordDays    *int    `json:"passwordDays,omitempty"`
	IdleLockMinutes *int    `json:"idleLockMinutes,omitempty"` // Deprecated: removed with the TUI change, ignored
}

type RecoveryKeyReply struct {
	RecoveryKey string `json:"recoveryKey"`
}

type GrantReply struct {
	Grant string `json:"grant"`
}
