package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/platform"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
	"github.com/spf13/cobra"
)

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "login",
		Short:        "Log this device in to your dokky account",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			var start client.LoginStart
			if err := call("login.start", nil, &start); err != nil {
				return err
			}
			if !start.Resume {
				fmt.Fprintf(out, "Open %s\nand check that it shows the code %s\n", start.VerifyURL, start.UserCode)
			}
			var poll client.LoginPoll
			for {
				if err := call("login.poll", nil, &poll); err != nil {
					return fmt.Errorf("login is not finished: %w; run dokky login again to retry", err)
				}
				switch poll.Status {
				case "denied":
					return errors.New("the login was denied in the browser")
				case "expired":
					return errors.New("the login code expired, run dokky login again")
				case "pending":
					time.Sleep(time.Duration(max(start.Interval, 1)) * time.Second)
					continue
				case "vault":
					// the master password is typed in the app, never on a command line
					return errors.New("signing in needs your master password: run dokky to finish")
				}
				break
			}
			in := bufio.NewReader(cmd.InOrStdin())
			for clashes := poll.Clashes; len(clashes) > 0; {
				c := clashes[0]
				keep, err := askKeep(in, out, c)
				if err != nil {
					return fmt.Errorf("login is not finished, %d name clashes left; run dokky login again", len(clashes))
				}
				var r client.LoginPoll
				if err := call("login.resolve", map[string]string{"kind": c.Kind, "name": c.Name, "keep": keep}, &r); err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), err)
					continue
				}
				clashes = r.Clashes
			}
			_, err := fmt.Fprintln(out, "Logged in.")
			return err
		},
	}
}

func askKeep(in *bufio.Reader, out io.Writer, c api.Clash) (string, error) {
	fmt.Fprintf(out, "\nThis device and your account both have a %s named %q.\n  local:   %s\n  account: %s\n", c.Kind, c.Name, c.Local, c.Account)
	fmt.Fprintln(out, "local replaces the account's copy on every device; account replaces this device's copy.")
	for {
		fmt.Fprint(out, "Keep which one? [l]ocal / [a]ccount: ")
		line, err := in.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "l", dsync.KeepLocal:
			return dsync.KeepLocal, nil
		case "a", dsync.KeepAccount:
			return dsync.KeepAccount, nil
		}
		if err != nil {
			fmt.Fprintln(out)
			return "", err
		}
	}
}

func newLogoutCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:          "logout",
		Short:        "Remove this device from your account and erase dokky's data on it",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			var st daemon.StatusReply
			if err := call("status", nil, &st); err != nil {
				return err
			}
			if st.Locked {
				return errors.New("dokky is locked: run dokky and unlock it first")
			}
			if !st.LoggedIn {
				_, err := fmt.Fprintln(out, "Not logged in.")
				return err
			}
			if n := uploads(st); force && n > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "%d unsent uploads will be discarded\n", n)
			}
			state, err := platform.StateDir()
			if err != nil {
				return err
			}
			c, err := ipc.Connect(state)
			if err != nil {
				return err
			}
			defer c.Close()
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			progress := make(chan struct{})
			go func() {
				defer close(progress)
				if force {
					return
				}
				if events, err := c.Events(ctx); err == nil {
					showUploads(cmd.ErrOrStderr(), events)
				}
			}()
			var r struct {
				Warning string `json:"warning"`
			}
			err = c.Call(ctx, "logout", map[string]bool{"force": force}, &r)
			stop()
			<-progress
			var ie *ipc.Error
			switch {
			case errors.Is(err, context.Canceled):
				return errors.New("stopped waiting; the removal continues in the background")
			case errors.As(err, &ie):
				return errors.New(ie.Message)
			case err != nil:
				return err
			}
			if r.Warning != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), r.Warning)
			}
			_, err = fmt.Fprintln(out, "Removed this device.")
			return err
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "erase right away without waiting for uploads")
	return cmd
}

func showUploads(w io.Writer, events <-chan ipc.Event) {
	last := 0
	for ev := range events {
		if ev.Event != "status" {
			continue
		}
		var st daemon.StatusReply
		if json.Unmarshal(ev.Data, &st) != nil {
			continue
		}
		n := uploads(st)
		if n > 0 && n != last {
			fmt.Fprintf(w, "Waiting for %d uploads to finish (Ctrl-C to stop)\n", n)
		}
		last = n
	}
}

func uploads(st daemon.StatusReply) int {
	n := 0
	for _, m := range st.Mounts {
		n += m.UploadsQueued + m.UploadsInProgress
	}
	return n
}
