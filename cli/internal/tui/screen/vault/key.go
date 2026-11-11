package vault

import (
	"image/color"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

var keyStops = []string{"btn:download", "btn:continue"}

// the recovery key, shown once in big groups, with a button that saves it as a file
type showKey struct {
	key    []rune // wiped once done; never logged or put in a toast
	at     string // the focused button
	saved  string // where the file went, "" until it is downloaded
	warned bool   // Continue was pressed once without a download
	done   bool
	err    string
	toast  string
}

// the file is written; the screen that asked for it takes the reply
type keySaved struct {
	from *showKey
	path string
	err  error
}

func newShowKey(key, toast string) *showKey {
	return &showKey{key: []rune(key), at: "btn:download", toast: toast}
}

func (k *showKey) update(msg tea.Msg, f *Flow, st *core.State) tea.Cmd {
	switch m := msg.(type) {
	case keySaved:
		if m.from != k {
			return nil
		}
		if m.err != nil {
			k.err = "Couldn't save the file: " + core.Report(m.err)
			return nil
		}
		k.saved, k.err, k.at = m.path, "", "btn:continue"
	case core.Click:
		if slices.Contains(keyStops, m.ID) {
			k.at = m.ID
			return k.press(st, f)
		}
	case tea.KeyPressMsg:
		switch s := m.String(); {
		case ui.Pressed(s, k.at):
			return k.press(st, f)
		case s == "d":
			k.at = "btn:download"
			return k.press(st, f)
		case ui.Walk(keyStops, &k.at, s):
		}
	}
	return nil
}

func (k *showKey) press(st *core.State, f *Flow) tea.Cmd {
	if k.at == "btn:download" {
		return k.download(st)
	}
	// a key written down on paper is fine too: a second Continue goes on without the file
	if k.saved == "" && !k.warned {
		k.warned, k.err = true, "Not downloaded yet. Press Continue again to skip."
		return nil
	}
	k.done = true
	var said tea.Cmd
	if k.toast != "" {
		said = core.Say(k.toast)
	}
	return tea.Batch(said, f.finish(st))
}

// written off the update loop; a second download replaces the first file rather than adding another
func (k *showKey) download(st *core.State) tea.Cmd {
	key, email, again, from := string(k.key), st.Snap.Account.Email, k.saved, k
	return func() tea.Msg {
		path, err := saveKey(again, key, email, time.Now())
		return keySaved{from: from, path: path, err: err}
	}
}

func (k *showKey) view(_ *core.State, win *ui.Window, maxH int) int {
	p := ui.P()
	win.Title = "Save your recovery key"
	sub(win, "Use it if you forget your master password.", k.err)
	win.Buttons = []ui.Btn{{ID: "download", Label: "Download", Primary: k.saved == ""}, {ID: "continue", Label: "Continue", Primary: k.saved != ""}}
	win.Active = win.Index(k.at)
	cw, bg := win.Inner(), p.Window
	body := ui.NewBox()
	chips := keyChips(string(k.key), cw, bg)
	body.Lines(0, 0, chips)
	y := len(chips)
	if win.Room(maxH) >= y+2 {
		l := ui.L(bg)
		if k.saved != "" {
			l.B(ui.G.Check, p.Kelp).Sp(1).T("Saved to "+core.Fit(tidy(k.saved), cw-10), p.Kelp)
		} else {
			l.T("dokky shows it only once.", p.Faint)
		}
		body.Put(0, y+1, l.Pad(cw))
		y += 2
	}
	win.Body = body
	return min(y, win.Room(maxH))
}

// DK1 and the groups after it as raised chips in two rows, letters spaced out when they fit
func keyChips(key string, w int, bg color.Color) []string {
	parts := strings.Split(key, "-")
	half := (len(parts) + 1) / 2
	groups := [][]string{parts[:half], parts[half:]}
	label := func(s string, spaced bool) string {
		if spaced {
			s = strings.Join(strings.Split(s, ""), " ")
		}
		return " " + s + " "
	}
	width := func(spaced bool) int {
		most := 0
		for _, g := range groups {
			n := 0
			for i, s := range g {
				if i > 0 {
					n += 2
				}
				n += ui.Width(label(s, spaced))
			}
			most = max(most, n)
		}
		return most
	}
	spaced := width(true) <= w
	// both rows start at one edge, so the groups line up in columns
	pad := max(0, (w-width(spaced))/2)
	var out []string
	for _, g := range groups {
		var top, mid, bot []string
		for _, s := range g {
			l := label(s, spaced)
			top = append(top, rim(ui.G.Top, ui.Width(l), ui.P().Cap, bg))
			mid = append(mid, ui.Paint(l, ui.P().Ramp[5], ui.P().Cap, true))
			bot = append(bot, rim(ui.G.Bottom, ui.Width(l), ui.P().Cap, bg))
		}
		gap := ui.Paint("  ", nil, bg, false)
		for _, row := range [][]string{top, mid, bot} {
			out = append(out, ui.L(bg).Sp(pad).Raw(strings.Join(row, gap)).Pad(w))
		}
	}
	return out
}

// the half-block rim of a raised fill; plain where half blocks can't be drawn
func rim(g string, n int, fill, under color.Color) string {
	if ui.G.ASCII {
		return ui.Paint(strings.Repeat(" ", n), nil, under, false)
	}
	return ui.Paint(strings.Repeat(g, n), fill, under, false)
}

func (k *showKey) actions(*core.State) []core.Action {
	enter := core.Action{Key: "enter", Label: "download", Accent: k.saved == ""}
	if k.at == "btn:continue" {
		enter = core.Action{Key: "enter", Label: "continue", Accent: k.saved != ""}
	}
	return []core.Action{enter, {Key: "← →", Label: "switch"}}
}

func (k *showKey) typing() bool               { return false }
func (k *showKey) holding() bool              { return !k.done }
func (k *showKey) animating(*core.State) bool { return false }

func (k *showKey) wipe() {
	clear(k.key)
	k.key = k.key[:0]
}
