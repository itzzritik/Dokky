package ui

import (
	"image/color"
	"strings"

	"github.com/itzzritik/dokky/cli/internal/api"
)

type Status struct {
	Icon, Words string
	Fg, WordsFg color.Color
}

func DriveStatus(d api.Drive, t float64) Status {
	p := P()
	switch d.State {
	case api.Connected:
		return Status{G.Check, "Connected", p.Kelp, p.Kelp}
	case api.Connecting:
		return Status{Spin(t), "Connecting", SpinFg(t), p.Accent}
	case api.Failed:
		host := d.Account
		if _, h, ok := strings.Cut(host, "@"); ok {
			host = h
		}
		return Status{G.Cross, "Can't reach " + host, p.Coral, p.Coral}
	case api.Removed:
		return Status{G.Cross, "Removed elsewhere", p.Faint, p.Muted}
	case api.Reconnect:
		return Status{G.Bang, "Reconnect to apply", p.Buoy, p.Buoy}
	}
	return Status{G.Dash, "Not connected", p.Faint, p.Muted}
}

// icon then words, for the right end of a row or tile
func (s Status) Line(bg color.Color, words bool) *Line {
	l := L(bg).B(s.Icon, s.Fg)
	if words {
		l.Sp(1).T(s.Words, s.WordsFg)
	}
	return l
}
