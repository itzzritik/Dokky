package main

import (
	"fmt"

	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/spf13/cobra"
)

func newMountCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "mount <name>",
		Short:        "Mount a drive",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var m daemon.MountStatus
			if err := call("mount", map[string]string{"name": args[0]}, &m); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s is mounted at %s\n", m.Name, m.MountPoint)
			return err
		},
	}
}

func newUnmountCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "unmount <name>",
		Short:        "Unmount a drive",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := call("unmount", map[string]string{"name": args[0]}, nil); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s is unmounted\n", args[0])
			return err
		},
	}
}
