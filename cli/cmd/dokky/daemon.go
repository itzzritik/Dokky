package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"syscall"
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
	startWait       = 90 * time.Second
	slowStartNote   = 2 * time.Second
	taskRestartWait = 5 * time.Second
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
	if errors.Is(err, errNotRunning) {
		if err = startDaemon(); err == nil {
			err = callOnce(method, params, result)
		}
	}
	if isCode(err, "protocol_mismatch") {
		_ = callOnce("daemon.restart", nil, nil)
		start, relaunched := time.Now(), runtime.GOOS != "windows"
		for deadline := start.Add(restartWait); time.Now().Before(deadline); {
			time.Sleep(250 * time.Millisecond)
			if !relaunched && time.Since(start) > taskRestartWait {
				relaunched = true
				if socketUp() != nil {
					_ = daemon.StartService()
				}
			}
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
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return errNotRunning
		}
		return err
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

func socketUp() error {
	state, err := platform.StateDir()
	if err != nil {
		return err
	}
	c, err := ipc.Connect(state)
	if err != nil {
		return err
	}
	return c.Close()
}

func startDaemon() error {
	bin := daemon.DefaultPaths().Bin
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("dokky is not installed at %s, install it with: npm i -g dokky, or run: dokky daemon", bin)
	}
	if !daemon.ServiceInstalled() || !daemon.ServiceCurrent(bin) {
		if err := daemon.InstallService(bin); err != nil {
			return fmt.Errorf("could not install the dokky service: %w", err)
		}
	}
	if err := daemon.StartService(); err != nil {
		return fmt.Errorf("could not start the dokky service: %w", err)
	}
	noted := false
	for start := time.Now(); time.Since(start) < startWait; time.Sleep(250 * time.Millisecond) {
		if socketUp() == nil {
			return nil
		}
		if !noted && time.Since(start) > slowStartNote {
			noted = true
			fmt.Fprintln(os.Stderr, "starting dokky...")
		}
	}
	return errors.New("dokky daemon did not start in time, check: dokky daemon")
}
