// Package sysauth asks the operating system to confirm the person at the computer: Touch ID or the Mac password,
// Windows Hello, or polkit. it is a gate, not a key: nothing is derived from it, the device key still opens the vault
package sysauth

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	ErrCancelled   = errors.New("the system prompt was cancelled")
	ErrUnavailable = errors.New("system authentication is not available here")
)

const (
	promptTimeout = 2 * time.Minute
	checkTimeout  = 15 * time.Second
	// powershell takes a second to start, and the state call asks often
	cacheFor = 5 * time.Minute
)

// Available says whether a system prompt can be shown and the name to show for it. remote is true when the TUI
// runs over SSH: the prompt would open on a screen the person cannot see
func Available(remote bool) (name string, ok bool) {
	if remote || overSSH() {
		return "", false
	}
	return checker.get()
}

// Prompt shows the system prompt and waits. nil means the system confirmed it is the user
func Prompt(ctx context.Context, reason string) error {
	if overSSH() {
		return ErrUnavailable
	}
	if strings.TrimSpace(reason) == "" {
		reason = "confirm it is you"
	}
	ctx, cancel := context.WithTimeout(ctx, promptTimeout)
	defer cancel()
	err := prompt(ctx, reason)
	if err != nil && ctx.Err() != nil && !errors.Is(err, ErrUnavailable) {
		return ErrCancelled
	}
	return err
}

// the daemon's own session: one started from an SSH login has no screen to show a prompt on
func overSSH() bool {
	return getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != ""
}

var (
	getenv   = os.Getenv
	lookPath = exec.LookPath
	checker  = &cache{check: available}
	clock    = time.Now
)

type cache struct {
	check func(context.Context) (string, bool)
	mu    sync.Mutex
	name  string
	ok    bool
	at    time.Time
}

// one check at a time, so a burst of state calls starts one helper, not many
func (c *cache) get() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && clock().Sub(c.at) < cacheFor {
		return c.name, c.ok
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	c.name, c.ok = c.check(ctx)
	c.at = clock()
	return c.name, c.ok
}

// a helper process; tests swap run to see what would start without starting it
type command struct {
	path  string
	args  []string
	env   []string // added to the daemon's environment
	raise bool     // bring the prompt window to the front once it shows
}

type result struct {
	out    string
	stderr string
	code   int // -1 when the process did not finish on its own
}

var run = execRun

// a helper that exits non-zero is a result, not an error; err is for one that could not run or was stopped
func execRun(ctx context.Context, c command) (result, error) {
	cmd := exec.CommandContext(ctx, c.path, c.args...)
	cmd.Env = append(os.Environ(), c.env...)
	cmd.WaitDelay = time.Second
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	prepare(cmd)
	if err := cmd.Start(); err != nil {
		return result{code: -1}, err
	}
	stop := started(c, cmd)
	err := cmd.Wait()
	stop()
	res := result{out: strings.TrimSpace(out.String()), stderr: strings.TrimSpace(stderr.String()), code: -1}
	if cmd.ProcessState != nil {
		res.code = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return res, err
	}
	return res, nil
}
