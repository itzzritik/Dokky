package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/engine"
)

type transfer struct {
	Mount string             `json:"mount"`
	Items []engine.QueueItem `json:"items"`
}

type transfers struct {
	list []transfer
}

func (a *app) xfersView() string {
	var b strings.Builder
	for _, t := range a.xfers.list {
		if len(t.Items) == 0 {
			continue
		}
		var up int
		for _, it := range t.Items {
			if it.Uploading {
				up++
			}
		}
		fmt.Fprintf(&b, "%s: %d uploading, %d queued\n", bold.Render(t.Mount), up, len(t.Items)-up)
		for _, it := range t.Items {
			word, when := "queued   ", ""
			if it.Uploading {
				word = "uploading"
			} else if wait := time.Until(it.Expiry).Round(time.Second); wait > 0 {
				when = "  starts in " + wait.String()
			}
			fmt.Fprintf(&b, "  %s  %-10s  %s%s\n", word, size(it.Size), it.Name, faint.Render(when))
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return "Nothing is waiting to upload.\n"
	}
	return b.String()
}
