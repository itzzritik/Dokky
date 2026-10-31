package main

import (
	"fmt"
	"os"

	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "dokky",
		Short:   "Mount your cloud storage as folders",
		Version: buildinfo.Version,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			switch cmd.Name() {
			case "daemon", "selftest":
				return nil
			}
			if err := ensurePath(); err != nil {
				fmt.Fprintln(os.Stderr, "dokky: could not update PATH:", err)
			}
			return nil
		},
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println("TUI comes in Part 4")
		},
	}
	cmd.SetVersionTemplate("dokky {{.Version}}\n")
	cmd.AddCommand(newDaemonCmd(), newMountCmd(), newUnmountCmd(), newStatusCmd())
	return cmd
}

func ensurePath() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	state, err := platform.StateDir()
	if err != nil {
		return err
	}
	_, err = platform.EnsurePath(home, state)
	return err
}
