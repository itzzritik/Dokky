package ui

import (
	"image/color"
	"slices"
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
		return Status{G.Check, "Mounted", p.Kelp, p.Kelp}
	case api.Connecting:
		return Status{Spin(t), "Mounting", SpinFg(t), p.Accent}
	case api.Failed:
		return Status{G.Cross, failWords(d.Error), p.Coral, p.Coral}
	case api.Removed:
		return Status{G.Cross, "Removed elsewhere", p.Faint, p.Muted}
	case api.Reconnect:
		return Status{G.Bang, "Remount to apply", p.Buoy, p.Buoy}
	}
	return Status{G.Dash, "Not mounted", p.Faint, p.Muted}
}

var (
	offline = []string{"no such host", "dial tcp", "connection refused", "connection reset", "timeout", "timed out", "did not answer", "unreachable", "tls", "eof", "502", "503", "504", "unavailable"}
	signIn  = []string{"login", "unauthorized", "401", "invalid_grant", "token", "expired", "auth", "password", "credential", "access key", "signature"}
)

// why a drive failed in a word or two; network first, since a sign-in that can't reach the service also says login
func failWords(err string) string {
	e := strings.ToLower(err)
	has := func(words []string) bool {
		return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(e, w) })
	}
	switch {
	case has(offline):
		return "Unreachable"
	case has(signIn):
		return loginFailed
	}
	return "Mount failed"
}

const loginFailed = "Login failed"

// signing in again is what this drive needs next
func LoginFailed(d api.Drive) bool { return d.State == api.Failed && failWords(d.Error) == loginFailed }

// icon then words, for the right end of a row or tile
func (s Status) Line(bg color.Color, words bool) *Line {
	l := L(bg).B(s.Icon, s.Fg)
	if words {
		l.Sp(1).T(s.Words, s.WordsFg)
	}
	return l
}
