package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/itzzritik/dokky/cli/internal/vault"
	"github.com/spf13/cobra"
)

func newSelftestCmd() *cobra.Command {
	var state string
	cmd := &cobra.Command{
		Use:          "selftest",
		Hidden:       true,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := selftest(state); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "selftest:", err)
				return err
			}
			return nil
		},
		SilenceErrors: true,
	}
	cmd.Flags().StringVar(&state, "state", "", "state directory")
	_ = cmd.MarkFlagRequired("state")
	return cmd
}

func selftest(state string) error {
	if err := checkState(state); err != nil {
		return err
	}
	cache, err := os.MkdirTemp("", "dokky-selftest-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(cache)
	e, err := engine.Start(cache, nil)
	if err != nil {
		return err
	}
	e.Stop()
	return nil
}

// the new binary must read what the old one wrote. a v2 store needs the master password, so only vault.json and
// the store's shape are checked; a v1 store is opened with the keychain store key as before
func checkState(state string) error {
	_, vaultErr := vault.Load(state)
	if vaultErr != nil && !errors.Is(vaultErr, os.ErrNotExist) {
		return vaultErr
	}
	v, err := store.FormatOf(state)
	if err != nil {
		return err
	}
	switch v {
	case 1:
		s, err := store.OpenLegacy(state, store.SystemKeyring(state))
		if err != nil {
			return err
		}
		_, err = s.Load()
		return err
	case 2:
		if vaultErr != nil {
			return errors.New("store.enc is locked with the master password but vault.json is missing")
		}
		return store.CheckV2(state)
	}
	return nil
}
