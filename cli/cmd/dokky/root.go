package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/itzzritik/dokky/cli/internal/buildinfo"
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
			case "daemon", "selftest", "doctor":
				return nil
			}
			if err := ensurePath(); err != nil {
				fmt.Fprintln(os.Stderr, "dokky: could not update PATH:", err)
			}
			return nil
		},
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(*cobra.Command, []string) error {
			d, err := connectTUI()
			if err != nil {
				return err
			}
			defer d.main.Close()
			defer d.long.Close()
			return tui.Run(d)
		},
	}
	cmd.SetVersionTemplate("dokky {{.Version}}\n")
	cmd.AddCommand(newDaemonCmd(), newMountCmd(), newUnmountCmd(), newStatusCmd(), newDoctorCmd(), newLoginCmd(), newLogoutCmd(), newSelftestCmd())
	return cmd
}

// long calls get their own connection: the daemon answers one request at a time per connection
type tuiDaemon struct{ main, long *ipc.Client }

func connectTUI() (tuiDaemon, error) {
	if err := call("status", nil, nil); err != nil {
		return tuiDaemon{}, err
	}
	state, err := platform.StateDir()
	if err != nil {
		return tuiDaemon{}, err
	}
	main, err := ipc.Connect(state)
	if err != nil {
		return tuiDaemon{}, err
	}
	long, err := ipc.Connect(state)
	if err != nil {
		main.Close()
		return tuiDaemon{}, err
	}
	return tuiDaemon{main, long}, nil
}

func (d tuiDaemon) Call(ctx context.Context, method string, params, result any) error {
	c := d.main
	switch method {
	case "service.configure", "service.test", "browse":
		c = d.long
	}
	err := c.Call(ctx, method, params, result)
	var ie *ipc.Error
	if errors.As(err, &ie) {
		return errors.New(ie.Message)
	}
	return err
}

func (d tuiDaemon) Events(ctx context.Context) (<-chan ipc.Event, error) { return d.main.Events(ctx) }

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
