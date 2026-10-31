package main

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/rclone/rclone/fs"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Show every mount and the daemon",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var st daemon.StatusReply
			if err := call("status", nil, &st); err != nil {
				return err
			}
			_, err := io.WriteString(cmd.OutOrStdout(), formatStatus(st))
			return err
		},
	}
}

func formatStatus(st daemon.StatusReply) string {
	var b strings.Builder
	fmt.Fprintf(&b, "dokky daemon %s\n\n", st.Version)
	if len(st.Mounts) == 0 {
		b.WriteString("No mounts yet. Run dokky to add one.\n")
	} else {
		rows := [][]string{{"NAME", "STATE", "MOUNT POINT", "CACHE", "UPLOADS"}}
		for _, m := range st.Mounts {
			rows = append(rows, []string{m.Name, m.State, m.MountPoint, fs.SizeSuffix(m.CacheBytes).ByteUnit(),
				fmt.Sprintf("%d queued, %d uploading", m.UploadsQueued, m.UploadsInProgress)})
		}
		writeTable(&b, rows)
		for _, m := range st.Mounts {
			if m.Error != "" {
				fmt.Fprintf(&b, "\n%s: %s\n", m.Name, m.Error)
			}
		}
	}
	fmt.Fprintf(&b, "\nFree space for the cache: %s", fs.SizeSuffix(st.FreeBytes).ByteUnit())
	if st.FreeBytes < st.MinFreeBytes {
		fmt.Fprintf(&b, ", below the %s dokky keeps free", fs.SizeSuffix(st.MinFreeBytes).ByteUnit())
	}
	b.WriteString("\n")
	return b.String()
}

func writeTable(b *strings.Builder, rows [][]string) {
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			widths[i] = max(widths[i], utf8.RuneCountInString(c))
		}
	}
	for _, r := range rows {
		last := len(r) - 1
		for i, c := range r[:last] {
			fmt.Fprintf(b, "%-*s  ", widths[i], c)
		}
		b.WriteString(r[last] + "\n")
	}
}
