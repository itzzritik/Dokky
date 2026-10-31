package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	var to string
	var yes bool
	cmd := &cobra.Command{
		Use:          "update",
		Short:        "Update dokky now",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			var st daemon.StatusReply
			if err := call("status", nil, &st); err != nil {
				return err
			}
			if n := uploads(st); n > 0 && !yes {
				fmt.Fprintf(out, "%d uploads are not finished and dokky restarts to update. Update now? [y/N]: ", n)
				line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
					_, err := fmt.Fprintln(out, "Not updated.")
					return err
				}
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
			var r struct {
				Version string `json:"version"`
				Updated bool   `json:"updated"`
			}
			err = c.Call(ctx, "update", map[string]string{"to": to}, &r)
			var ie *ipc.Error
			switch {
			case errors.Is(err, context.Canceled):
				return errors.New("update stopped, nothing was changed")
			case errors.As(err, &ie):
				return errors.New(ie.Message)
			case err != nil:
				return err
			case !r.Updated:
				_, err = fmt.Fprintf(out, "dokky %s is up to date.\n", r.Version)
			default:
				_, err = fmt.Fprintf(out, "Installed dokky %s, restarting onto it.\n", r.Version)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "install exactly this version and turn auto update off")
	cmd.Flags().BoolVar(&yes, "yes", false, "don't ask when uploads are not finished")
	return cmd
}
