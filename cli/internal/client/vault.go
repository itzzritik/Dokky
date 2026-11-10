package client

import (
	"context"

	"github.com/itzzritik/dokky/cli/internal/api"
)

// Argon2id takes about a second; setup and join also seal and upload every drive
const vaultSlow = slow

// creates the master password and returns the recovery key, shown once
func (c *Client) VaultSetup(ctx context.Context, password string) (string, error) {
	r, err := get[api.RecoveryKeyReply](c, ctx, vaultSlow, api.MVaultSetup, api.PasswordParams{Password: password})
	return r.RecoveryKey, err
}

// system waits for the person at the computer, so it gets the long timeout
func (c *Client) VaultUnlock(ctx context.Context, p api.UnlockParams) error {
	return c.call(ctx, long, api.MVaultUnlock, p, nil)
}

func (c *Client) VaultRecover(ctx context.Context, recoveryKey, newPassword string) error {
	return c.call(ctx, vaultSlow, api.MVaultRecover, api.RecoverParams{RecoveryKey: recoveryKey, NewPassword: newPassword}, nil)
}

func (c *Client) VaultLock(ctx context.Context) error {
	return c.call(ctx, slow, api.MVaultLock, nil, nil)
}

// Deprecated: removed with the TUI change; the daemon no longer answers vault.authorize
func (c *Client) VaultAuthorize(ctx context.Context, p api.UnlockParams) (string, error) {
	r, err := get[api.GrantReply](c, ctx, long, api.MVaultAuthorize, p)
	return r.Grant, err
}

func (c *Client) VaultSettings(ctx context.Context, p api.VaultSettingsParams) error {
	return c.call(ctx, quick, api.MVaultSettings, p, nil)
}

func (c *Client) VaultChangePassword(ctx context.Context, current, next string) error {
	return c.call(ctx, vaultSlow, api.MVaultChangePassword, api.ChangePasswordParams{Current: current, Next: next}, nil)
}

func (c *Client) VaultNewRecoveryKey(ctx context.Context, password string) (string, error) {
	r, err := get[api.RecoveryKeyReply](c, ctx, vaultSlow, api.MVaultNewRecoveryKey, api.PasswordParams{Password: password})
	return r.RecoveryKey, err
}

func (c *Client) VaultJoin(ctx context.Context, password string) error {
	return c.call(ctx, vaultSlow, api.MVaultJoin, api.PasswordParams{Password: password}, nil)
}

func (c *Client) VaultJoinRecover(ctx context.Context, recoveryKey, newPassword string) error {
	return c.call(ctx, vaultSlow, api.MVaultJoinRecover, api.RecoverParams{RecoveryKey: recoveryKey, NewPassword: newPassword}, nil)
}

// SessionOpen proves it is the user, or reuses a live session; system waits for the person, so the long timeout
func (c *Client) SessionOpen(ctx context.Context, p api.SessionParams) error {
	return c.call(ctx, long, api.MSessionOpen, p, nil)
}

func (c *Client) SessionClose(ctx context.Context) error {
	return c.call(ctx, quick, api.MSessionClose, nil, nil)
}
