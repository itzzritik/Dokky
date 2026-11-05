package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/doctor"
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
			_ = call(api.MState, nil, nil)
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

var checkNames = map[string]string{
	"fuse3": "File system driver", "winfsp": "File system driver", "macos version": "macOS",
	"daemon installed": "Background service", "daemon running": "Service running", "daemon version": "Service version",
	"keychain": "Keychain", "disk space": "Free space",
}

// the doctor's checks in plain words; problems get a short status and the fix in the detail
func doctorChecks(ctx context.Context) []tui.Check {
	var out []tui.Check
	for _, c := range doctor.Run(ctx, realDoctorEnv()) {
		ch := tui.Check{Name: cmp.Or(checkNames[c.Name], c.Name), Status: upper(c.Detail), Detail: upper(c.Detail) + ".", Tone: tui.Good}
		switch {
		case c.Warn:
			ch.Tone, ch.Status = tui.Warn, "Needs a look"
		case !c.OK:
			ch.Tone, ch.Status = tui.Bad, "Not working"
		}
		if c.Fix != "" {
			ch.Detail += "\n\nTo fix it: " + c.Fix
		}
		out = append(out, ch)
	}
	return out
}

func upper(s string) string {
	if s == "" || strings.HasPrefix(s, "macOS") {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func uninstall() error {
	out, err := exec.Command("npm", "rm", "-g", "dokky").CombinedOutput()
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
