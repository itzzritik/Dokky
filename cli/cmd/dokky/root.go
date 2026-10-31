package main

import (
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "dokky",
		Short:   "Mount your cloud storage as folders",
		Version: buildinfo.Version,
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println("TUI comes in Part 4")
		},
	}
	cmd.SetVersionTemplate("dokky {{.Version}}\n")
	return cmd
}
