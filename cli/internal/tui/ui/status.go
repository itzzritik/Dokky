package ui

import (
	"image/color"

	"github.com/itzzritik/dokky/cli/internal/api"
)

type Status struct {
	Icon, Words string
	Fg, WordsFg color.Color
	Bar         float64 // drawn as a short bar instead of the icon when Barred
	Barred      bool
	T           float64
}

func DriveStatus(d api.Drive, t float64) Status {
	p := P()
	switch d.State {
	case api.Connected:
		return Status{Icon: G.Check, Words: "Mounted", Fg: p.Kelp, WordsFg: p.Kelp}
	case api.Connecting:
		return Status{Icon: Spin(t), Words: "Mounting", Fg: SpinFg(t), WordsFg: p.Accent}
	case api.Failed:
		return Status{Icon: G.Cross, Words: failWords(d.Error), Fg: p.Coral, WordsFg: p.Coral}
	case api.Leaving:
		return Status{Icon: G.Dash, Words: "Leaving", Fg: p.Buoy, WordsFg: p.Buoy}
	case api.Removing:
		return Status{Words: "Removing", WordsFg: p.Accent, Bar: d.Progress, Barred: true, T: t}
	case api.Reconnect:
		return Status{Icon: G.Bang, Words: "Remount to apply", Fg: p.Buoy, WordsFg: p.Buoy}
	}
	return Status{Icon: G.Dash, Words: "Not mounted", Fg: p.Faint, WordsFg: p.Muted}
}

// why a drive failed in a word or two
func failWords(err string) string {
	switch api.FailKind(err) {
	case "offline":
		return "Unreachable"
	case "signin":
		return loginFailed
	}
	return "Mount failed"
}

const loginFailed = "Login failed"

// signing in again is what this drive needs next
func LoginFailed(d api.Drive) bool { return d.State == api.Failed && failWords(d.Error) == loginFailed }

// icon then words, for the right end of a row or tile
func (s Status) Line(bg color.Color, words bool) *Line {
	l := L(bg)
	if s.Barred {
		l.Raw(ThinBar(6, s.Bar, s.T, nil, bg))
	} else {
		l.B(s.Icon, s.Fg)
	}
	if words {
		l.Sp(1).T(s.Words, s.WordsFg)
	}
	return l
}
