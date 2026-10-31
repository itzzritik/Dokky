package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/store"
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
	if _, err := os.Stat(filepath.Join(state, "store.enc")); err == nil {
		s, err := store.Open(state, store.SystemKeyring(state))
		if err != nil {
			return err
		}
		if _, err := s.Load(); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
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
