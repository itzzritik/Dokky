package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/daemon"
)

// a dev build runs the background service from its own binary, so ./cli/dokky tests the daemon too;
// opening the installed dokky later takes the service back, in useInstalledDaemon
func useDevDaemon(note io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// a go test binary would stop the real service, then run its tests again instead of a daemon
	if strings.HasSuffix(strings.TrimSuffix(exe, ".exe"), ".test") {
		return errors.New("not from a test binary")
	}
	var s api.State
	if callOnce(api.MState, nil, &s) == nil && s.Build == buildinfo.Stamp() {
		return nil
	}
	fmt.Fprintln(note, "dokky: running the background service from this build...")
	// the installed service goes first, or launchd brings it straight back
	if err := daemon.StopService(); err != nil {
		return fmt.Errorf("stop the installed service: %w", err)
	}
	_ = callOnce("daemon.restart", nil, nil)
	for deadline := time.Now().Add(restartWait); socketUp() == nil; time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			return errors.New("the running dokky service did not stop")
		}
	}
	cmd := exec.Command(exe, "daemon")
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	for deadline := time.Now().Add(startWait); socketUp() != nil; time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			return errors.New("the dev service did not start, run: ./cli/dokky daemon")
		}
	}
	return nil
}

// the installed dokky takes the background service back from a dev build's daemon as it opens, so the dokky
// command always runs on the installed one: the dev daemon is asked to exit and the installed service starts.
// a daemon of another installed version is left to Health, which restarts it once nothing is uploading
func useInstalledDaemon(note io.Writer) error {
	var s api.State
	if callOnce(api.MState, nil, &s) != nil || !strings.Contains(s.Version, "dev") {
		return nil
	}
	fmt.Fprintln(note, "dokky: switching back to the installed background service...")
	_ = callOnce("daemon.restart", nil, nil)
	for deadline := time.Now().Add(restartWait); socketUp() == nil; time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			return errors.New("the dev build's background service did not stop")
		}
	}
	return startDaemon(note)
}
