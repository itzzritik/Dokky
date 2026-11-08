package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strconv"
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
	var after int
	cmd := &cobra.Command{
		Use:          "daemon",
		Short:        "Run the dokky daemon in the foreground",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if after > 0 {
				return relaunch(after)
			}
			err := daemon.Run(cmd.Context(), daemon.DefaultPaths())
			if errors.Is(err, daemon.ErrRestart) {
				relaunchWhenGone()
				os.Exit(restartExitCode)
			}
			return err
		},
	}
	cmd.Flags().IntVar(&after, "relaunch-after", 0, "")
	_ = cmd.Flags().MarkHidden("relaunch-after")
	return cmd
}

// launchd and systemd restart the daemon by themselves, but Task Scheduler leaves a task that exited stopped
func relaunchWhenGone() {
	if runtime.GOOS != "windows" || !daemon.ServiceInstalled() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "daemon", "--relaunch-after", strconv.Itoa(os.Getpid()))
	detach(cmd)
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

func relaunch(pid int) error {
	if p, err := os.FindProcess(pid); err == nil {
		_, _ = p.Wait()
	}
	return startDaemon(io.Discard)
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

const updatedEnv = "DOKKY_UPDATED"

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

// the service starts again from the installed dokky, set up first if it isn't; never stopped when nothing could start it again
func restartService() error {
	if bin := daemon.DefaultPaths().Bin; !isFile(bin) {
		return fmt.Errorf("dokky is not installed at %s, install it with: npm i -g dokky", bin)
	}
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
	noted, ran := false, time.Now()
	for start := time.Now(); time.Since(start) < startWait; time.Sleep(250 * time.Millisecond) {
		if socketUp() == nil {
			return nil
		}
		// Task Scheduler ignores a run while the old instance is still exiting
		if runtime.GOOS == "windows" && time.Since(ran) > taskRestartWait {
			_ = daemon.StartService()
			ran = time.Now()
		}
		if !noted && time.Since(start) > slowStartNote {
			noted = true
			fmt.Fprintln(note, "starting dokky...")
		}
	}
	return errors.New("dokky daemon did not start in time, check: dokky daemon")
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
