package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/spf13/cobra"
)

const (
	restartExitCode = 75
	callTimeout     = 2 * time.Minute
	restartWait     = 30 * time.Second
)

var errNotRunning = errors.New("dokky daemon is not running, start it with: dokky daemon")

func newDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "daemon",
		Short:        "Run the dokky daemon in the foreground",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := daemon.Run(cmd.Context(), daemon.DefaultPaths())
			if errors.Is(err, daemon.ErrRestart) {
				os.Exit(restartExitCode)
			}
			return err
		},
	}
}

func call(method string, params, result any) error {
	err := callOnce(method, params, result)
	if isCode(err, "protocol_mismatch") {
		_ = callOnce("daemon.restart", nil, nil)
		for deadline := time.Now().Add(restartWait); time.Now().Before(deadline); {
			time.Sleep(250 * time.Millisecond)
			err = callOnce(method, params, result)
			if !errors.Is(err, errNotRunning) && !isCode(err, "protocol_mismatch") {
				break
			}
		}
	}
	var ie *ipc.Error
	if errors.As(err, &ie) {
		return errors.New(ie.Message)
	}
	return err
}

func callOnce(method string, params, result any) error {
	state, err := platform.StateDir()
	if err != nil {
		return err
	}
	c, err := ipc.Connect(state)
	if err != nil {
		return errNotRunning
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return c.Call(ctx, method, params, result)
}

func isCode(err error, code string) bool {
	var ie *ipc.Error
	return errors.As(err, &ie) && ie.Code == code
}
