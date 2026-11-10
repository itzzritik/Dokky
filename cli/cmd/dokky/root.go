package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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
		Short:   "Mount your storage as native drives",
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
			return nil
		},
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// nothing waits before the TUI: it starts the service, updates and fixes problems in the background
			start := func() error { return startDaemon(io.Discard) }
			if buildinfo.Dev() {
				start = func() error { return useDevDaemon(io.Discard) }
				if err := useDevDaemon(os.Stderr); err != nil {
					fmt.Fprintln(os.Stderr, "dokky:", err)
				}
			}
			// over SSH nobody at this computer can answer Touch ID or Windows Hello, so the TUI never offers it
			remote := os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
			err := tui.Run(cmd.Context(), tui.Deps{Reopened: os.Getenv(updatedEnv) != "", Remote: remote, Start: start, Doctor: doctorChecks, Uninstall: uninstall})
			if errors.Is(err, tui.ErrReopen) {
				return reopen()
			}
			return err
		},
	}
	cmd.SetVersionTemplate("dokky {{.Version}}\n")
	// everything that changes dokky happens in the app, unlocked; the command line only looks
	cmd.AddCommand(newDaemonCmd(), newStatusCmd(), newDoctorCmd(), newSelftestCmd())
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
