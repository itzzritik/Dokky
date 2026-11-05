package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
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
		if err = startDaemon(os.Stderr); err == nil {
			err = callOnce(method, params, result)
		}
	}
	if isCode(err, "protocol_mismatch") {
		_ = callOnce("daemon.restart", nil, nil)
		awaitDaemon(func() bool {
			err = callOnce(method, params, result)
			return !errors.Is(err, errNotRunning) && !isCode(err, "protocol_mismatch")
		})
	}
	return messageOnly(err)
}

// polls until ok, relaunching the Windows task if the daemon does not come back by itself
func awaitDaemon(ok func() bool) {
	start, relaunched := time.Now(), runtime.GOOS != "windows"
	for deadline := start.Add(restartWait); time.Now().Before(deadline); {
		time.Sleep(250 * time.Millisecond)
		if !relaunched && time.Since(start) > taskRestartWait {
			relaunched = true
			if socketUp() != nil {
				_ = daemon.StartService()
			}
		}
		if ok() {
			return
		}
	}
}

// before the TUI: the daemon fetches a newer dokky and restarts onto it, so nothing changes under the open TUI; true once it has
func updateOnOpen(running string, note io.Writer) bool {
	var r struct {
		Restarting bool   `json:"restarting"`
		Version    string `json:"version"`
	}
	if running == "" || os.Getenv(updatedEnv) != "" {
		return false
	}
	done := make(chan error, 1)
	go func() { done <- callOnce("update.restart", nil, &r) }()
	var err error
	select {
	case err = <-done:
	case <-time.After(slowStartNote):
		fmt.Fprintln(note, "checking for a new dokky...")
		err = <-done
	}
	if err != nil || !r.Restarting {
		return false
	}
	fmt.Fprintln(note, "updating dokky to "+r.Version+"...")
	awaitDaemon(func() bool {
		var s api.State
		return callOnce(api.MState, nil, &s) == nil && s.Version != running
	})
	return true
}

const updatedEnv = "DOKKY_UPDATED"

// the installed dokky was just replaced under this process: run the new one in its place
func relaunch() {
	bin := daemon.DefaultPaths().Bin
	self, err := os.Executable()
	if err != nil || filepath.Clean(self) != filepath.Clean(bin) {
		return
	}
	_ = syscall.Exec(bin, os.Args, append(os.Environ(), updatedEnv+"=1"))
}

// runs dokky again in this terminal: in place where the OS allows it, else as a child whose exit is this one's
func reopen() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	env := append(os.Environ(), updatedEnv+"=1")
	if runtime.GOOS != "windows" {
		return syscall.Exec(exe, os.Args, env)
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Env = os.Stdin, os.Stdout, os.Stderr, env
	err = cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.ExitCode())
	}
	return err
}

// the service starts again from the installed dokky, set up first if it isn't
func restartService() error {
	_ = callOnce("daemon.restart", nil, nil)
	time.Sleep(time.Second)
	return startDaemon(io.Discard)
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

func startDaemon(note io.Writer) error {
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
			fmt.Fprintln(note, "starting dokky...")
		}
	}
	return errors.New("dokky daemon did not start in time, check: dokky daemon")
}
