package vault

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/stretchr/testify/require"
)

func coded(c, msg string) error { return &client.Error{Code: c, Message: msg} }

func TestExplainCodes(t *testing.T) {
	now := time.Now()
	v := api.Vault{System: "Windows Hello", PasswordDays: 14}
	for name, c := range map[string]struct {
		err  error
		v    api.Vault
		want string
	}{
		"wrong":           {coded(api.ErrWrongPassword, "wrong password"), v, "That password is not right."},
		"cancelled":       {coded(api.ErrCancelled, "cancelled"), v, "Cancelled. Try again or use your master password."},
		"no system":       {coded(api.ErrSystemUnavailable, "x"), v, "Windows Hello isn't available right now."},
		"password due":    {coded(api.ErrPasswordRequired, "your master password changed. Enter the new one to unlock dokky"), v, "Your master password changed. Enter the new one to unlock dokky."},
		"due, no reason":  {coded(api.ErrPasswordRequired, ""), v, "Type your master password this time."},
		"weak":            {coded(api.ErrWeakPassword, "use at least 8 characters"), v, "Use at least 8 characters."},
		"bad key":         {coded(api.ErrBadRecoveryKey, "x"), v, "That recovery key is not right."},
		"session ended":   {coded(api.ErrSessionRequired, "x"), v, "dokky locked meanwhile. Unlock it and try again."},
		"locked":          {coded(api.ErrLocked, "x"), v, "dokky is locked. Unlock it first."},
		"not running":     {client.ErrNotRunning, v, "dokky isn't running."},
		"cancel is quiet": {context.Canceled, v, ""},
		"plain":           {errors.New("could not reach the server"), v, "Could not reach the server."},
	} {
		require.Equal(t, c.want, explain(c.err, c.v, now), name)
	}
}

func TestSystemNames(t *testing.T) {
	for name, want := range map[string]string{"Touch ID": "Touch ID", "your Mac password": "Mac password", "your system password": "System password"} {
		require.Equal(t, want, systemChoice(api.Vault{System: name}))
	}
}

func TestOfferedOnlyWhereSomeoneCanAnswer(t *testing.T) {
	for name, c := range map[string]struct {
		v      api.Vault
		remote bool
		want   bool
	}{
		"here":         {api.Vault{System: "Touch ID"}, false, true},
		"over ssh":     {api.Vault{System: "Touch ID"}, true, false},
		"password due": {api.Vault{System: "Touch ID", PasswordDue: true}, false, false},
		"none":         {api.Vault{}, false, false},
	} {
		st := &core.State{Snap: api.State{Vault: c.v}, Deps: core.Deps{Remote: c.remote}}
		require.Equal(t, c.want, offered(st), name)
	}
}

func TestLabels(t *testing.T) {
	require.Equal(t, "7 days", DaysLabel(7))
	require.Equal(t, "15 days", DaysLabel(15))
}
