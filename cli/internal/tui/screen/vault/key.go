package vault

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// the recovery key, shown once in big groups; it goes on only after the person says it is saved
type showKey struct {
	key   []rune // wiped once saved; never logged or put in a toast
	saved bool
	done  bool
	err   string
	toast string
}

func newShowKey(key, toast string) *showKey { return &showKey{key: []rune(key), toast: toast} }

func (k *showKey) update(msg tea.Msg, f *Flow, st *core.State) tea.Cmd {
	id := ""
	switch m := msg.(type) {
	case core.Click:
		id = map[string]string{"btn:copy": "c", "btn:continue": "enter", "saved": "space"}[m.ID]
	case tea.KeyPressMsg:
		id = m.String()
	}
	switch id {
	case "c":
		// shown at once, even over the full screen setup
		return tea.Batch(tea.SetClipboard(string(k.key)), core.Send(core.Toast{Text: "Recovery key copied", Now: true}))
	case "space":
		k.saved, k.err = !k.saved, ""
	case "enter":
		if !k.saved {
			k.err = "Press space once your key is saved."
			return nil
		}
		k.done = true
		var said tea.Cmd
		if k.toast != "" {
			said = core.Say(k.toast)
		}
		return tea.Batch(said, f.finish(st))
	}
	return nil
}

func (k *showKey) view(_ *core.State, win *ui.Window, maxH int) int {
	p := ui.P()
	win.Title = "Save your recovery key"
	sub(win, "It gets you back in if you forget your password.", k.err)
	win.Buttons = []ui.Btn{{ID: "copy", Label: "Copy"}, {ID: "continue", Label: "Continue", Primary: true}}
	if k.saved {
		win.Active = 1
	}
	cw, bg := win.Inner(), p.Surface
	body := ui.NewBox()
	chips := keyChips(string(k.key), cw, bg)
	body.Lines(0, 0, chips)
	y := len(chips)
	switch room := win.Room(maxH); {
	case room >= y+4:
		body.Put(0, y+1, win.Text("dokky shows it only once. Keep it somewhere safe.", p.Faint))
		y += 3
	case room >= y+2:
		y++
	}
	box := ui.Paint("   ", nil, p.Cap, false)
	if k.saved {
		box = ui.Paint(" "+ui.G.Check+" ", p.OnAccent, p.Accent, true)
	}
	body.Hit(0, y, "saved", ui.L(bg).Raw(box).Sp(2).T("I saved my recovery key", p.Text).Pad(cw))
	win.Body = body
	return min(y+1, win.Room(maxH))
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
	return []core.Action{{Key: "c", Label: "copy"}, {Key: "space", Label: "I saved it"}, {Key: "enter", Label: "continue", Accent: k.saved}}
}

func (k *showKey) typing() bool               { return false }
func (k *showKey) holding() bool              { return !k.done }
func (k *showKey) animating(*core.State) bool { return false }

func (k *showKey) wipe() {
	clear(k.key)
	k.key = k.key[:0]
}
