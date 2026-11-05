package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/health"
	"github.com/spf13/cobra"
)

// the same checks and repairs as the Health tab's Fix all, without the TUI
func newFixCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "fix",
		Short:        "Find and fix problems on this computer",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, ctx := cmd.OutOrStdout(), cmd.Context()
			var s api.State
			if err := call(api.MState, nil, &s); err != nil {
				return messageOnly(err)
			}
			c, err := client.Dial(ctx)
			if err != nil {
				return err
			}
			act, _ := c.Activity(ctx)
			fmt.Fprintln(out, "Checking dokky...")
			st := &core.State{Snap: s, Act: act, C: c, Now: time.Now(), Checks: doctorChecks(ctx)}
			failed := 0
			for _, ch := range health.Repairs(st, false) {
				if err := ch.Repair(); err != nil {
					failed++
					fmt.Fprintf(out, "✗ %s: %s\n", ch.Name, core.Report(err))
					continue
				}
				fmt.Fprintf(out, "✓ %s: fixed (%s)\n", ch.Name, ch.Status)
			}
			left := 0
			for _, ch := range health.Checks(st) {
				if (ch.Tone == core.Warn || ch.Tone == core.Bad) && ch.Repair == nil {
					left++
					fmt.Fprintf(out, "! %s: %s. %s\n", ch.Name, ch.Status, ch.Fix)
				}
			}
			switch {
			case failed > 0:
				return errors.New("some problems could not be fixed")
			case left == 0:
				fmt.Fprintln(out, "Everything is working.")
			}
			return nil
		},
	}
}
