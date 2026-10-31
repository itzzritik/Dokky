package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/itzzritik/dokky/cli/internal/doctor"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/lib/diskusage"
	"github.com/spf13/cobra"
)

const (
	doctorHTTPTimeout = 5 * time.Second
	defaultMinFree    = 10 << 30
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "doctor",
		Short:        "Check this machine and show the exact fix for anything wrong",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env := realDoctorEnv()
			checks := doctor.Run(cmd.Context(), env)
			if _, err := io.WriteString(cmd.OutOrStdout(), formatDoctor(checks, env.LogPath)); err != nil {
				return err
			}
			for _, c := range checks {
				if !c.OK && !c.Warn {
					cmd.SilenceErrors = true
					return errors.New("doctor found problems")
				}
			}
			return nil
		},
	}
}

func formatDoctor(checks []doctor.Check, logPath string) string {
	var b strings.Builder
	for _, c := range checks {
		word := "ok"
		switch {
		case c.Warn:
			word = "warn"
		case !c.OK:
			word = "fail"
		}
		fmt.Fprintf(&b, "%-4s  %s: %s\n", word, c.Name, c.Detail)
		if c.Fix != "" {
			for i, line := range strings.Split(c.Fix, "\n") {
				prefix := "      "
				if i == 0 {
					prefix += "fix: "
				} else {
					prefix += "     "
				}
				b.WriteString(prefix + line + "\n")
			}
		}
	}
	fmt.Fprintf(&b, "\nLog file: %s\n", logPath)
	return b.String()
}

func realDoctorEnv() doctor.Env {
	state, _ := platform.StateDir()
	cache, _ := platform.CacheDir()
	env := doctor.Env{
		GOOS:            runtime.GOOS,
		LookPath:        exec.LookPath,
		Stat:            os.Stat,
		ReadFile:        os.ReadFile,
		Access:          doctor.OpenReadWrite,
		HTTPGet:         httpProbe,
		MacVersion:      macVersion,
		DaemonInstalled: daemon.ServiceInstalled(),
		Version:         buildinfo.Version,
		MinFreeBytes:    defaultMinFree,
		LogPath:         filepath.Join(state, "dokky.log"),
	}
	var st daemon.StatusReply
	env.DaemonVersion = func() (string, error) {
		if err := callOnce("status", nil, &st); err != nil {
			if errors.Is(err, errNotRunning) {
				return "", doctor.ErrNotRunning
			}
			return "", err
		}
		return st.Version, nil
	}
	if _, err := env.DaemonVersion(); err == nil {
		env.FreeBytes, env.MinFreeBytes = st.FreeBytes, st.MinFreeBytes
	} else {
		env.FreeBytes = freeBytesNear(cache)
	}
	env.KeyringFallback, env.KeyringErr = keyringState(store.SystemKeyring(state), state)
	return env
}

func freeBytesNear(dir string) uint64 {
	for ; ; dir = filepath.Dir(dir) {
		if du, err := diskusage.New(dir); err == nil {
			return du.Available
		}
		if dir == filepath.Dir(dir) {
			return math.MaxInt64
		}
	}
}

func keyringState(kr store.Keyring, state string) (fallback bool, err error) {
	if _, err := kr.Get(); errors.Is(err, store.ErrNotFound) {
		if _, statErr := os.Stat(filepath.Join(state, "store.enc")); statErr == nil {
			return false, store.ErrKeyMissing
		}
	} else if err != nil {
		return false, err
	}
	f, ok := kr.(store.Fallbacker)
	return ok && f.Fallback(), nil
}

func httpProbe(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), doctorHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
