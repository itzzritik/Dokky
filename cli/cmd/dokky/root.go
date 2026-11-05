package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/tui"
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
			case "doctor":
			default:
				if err := ensurePath(); err != nil {
					fmt.Fprintln(os.Stderr, "dokky: could not update PATH:", err)
				}
			}
			if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
				return nil
			}
			if err := platform.EnsureFinderHost(); err != nil {
				fmt.Fprintln(os.Stderr, "dokky: drives will show as localhost in Finder:", err)
			}
			return nil
		},
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// starts or restarts the daemon when it can; the TUI shows its own screen when it cannot
			var s api.State
			_ = call(api.MState, nil, &s)
			if updateOnOpen(s.Version, os.Stderr) {
				relaunch()
			}
			return tui.Run(cmd.Context(), tui.Deps{Start: func() error { return startDaemon(io.Discard) }, Doctor: doctorChecks, Uninstall: uninstall})
		},
	}
	cmd.SetVersionTemplate("dokky {{.Version}}\n")
	cmd.AddCommand(newDaemonCmd(), newMountCmd(), newUnmountCmd(), newStatusCmd(), newDoctorCmd(), newLoginCmd(), newLogoutCmd(), newSelftestCmd(), newUpdateCmd())
	return cmd
}

// prints only the message while errors.As still finds the code
type daemonError struct{ ie *ipc.Error }

func (e *daemonError) Error() string { return e.ie.Message }
func (e *daemonError) Unwrap() error { return e.ie }

func messageOnly(err error) error {
	var ie *ipc.Error
	if errors.As(err, &ie) {
		return &daemonError{ie}
	}
	return err
}

func uninstall() error {
	state, err := platform.StateDir()
	if err != nil {
		return err
	}
	cmd, err := daemon.NPM(context.Background(), state, "rm", "-g", "dokky")
	if err != nil {
		return err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
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
