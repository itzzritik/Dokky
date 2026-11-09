package settings

import (
	"cmp"
	"context"
	"slices"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// how much each drive keeps on this computer and where; Clear, or c, empties it
type storage struct {
	at string // the focused button
}

func openStorage(*core.State) tea.Cmd { return core.Open(&storage{at: "btn:close"}) }

// the bar, also listed by the actions menu; Clear shows while something is cached
func (s *storage) Buttons(st *core.State) []ui.Btn {
	btns := []ui.Btn{{ID: "close", Label: "Close"}}
	if st.Snap.CacheBytes > 0 {
		btns = append(btns, ui.Btn{ID: "clear", Label: "Clear", Danger: true})
	}
	return btns
}

func (s *storage) stops(st *core.State) []string {
	var ids []string
	for _, b := range s.Buttons(st) {
		ids = append(ids, "btn:"+b.ID)
	}
	return ids
}

func (s *storage) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	stops := s.stops(st)
	// once the cache is empty Clear goes, and focus with it
	if !slices.Contains(stops, s.at) {
		s.at = "btn:close"
	}
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		switch k := m.String(); {
		case k == "esc":
			return s, core.Close(s)
		case k == "c" && slices.Contains(stops, "btn:clear"):
			s.at = "btn:clear"
			return s, s.press(s.at)
		case ui.Pressed(k, s.at):
			return s, s.press(s.at)
		case ui.Walk(stops, &s.at, k):
		}
	case core.Click:
		if slices.Contains(stops, m.ID) {
			s.at = m.ID
			return s, s.press(m.ID)
		}
	}
	return s, nil
}

func (s *storage) press(id string) tea.Cmd {
	if id == "btn:clear" {
		return clearCache()
	}
	return core.Close(s)
}

func clearCache() tea.Cmd {
	return confirm.Open(&confirm.Confirm{
		Question: "Clear cached files?",
		Line:     "Mounted drives remount for a moment.",
		Do:       "Clear",
		Run: func(st *core.State) tea.Cmd {
			c := st.C
			return tea.Batch(core.Say("Clearing cached files"), func() tea.Msg {
				if err := c.ClearCache(context.Background()); err != nil {
					return core.Toast{Text: core.Report(err), Bad: true}
				}
				return core.Toast{Text: "Cached files cleared"}
			})
		},
	})
}

func (s *storage) window(st *core.State, maxW, maxH int) *ui.Window {
	p := ui.P()
	win := &ui.Window{W: min(64, maxW), Title: "Cached files", Buttons: s.Buttons(st)}
	win.Active = max(0, win.Index(s.at))
	cw := win.Inner()
	bg := p.Surface
	body := ui.NewBox()
	total := st.Snap.CacheBytes
	drives := slices.DeleteFunc(slices.Clone(st.Snap.Drives), func(d api.Drive) bool { return d.CacheBytes <= 0 })
	slices.SortFunc(drives, func(a, b api.Drive) int { return cmp.Compare(b.CacheBytes, a.CacheBytes) })
	if total <= 0 {
		body.Put(0, 0, ui.L(bg).B("Nothing cached yet", p.Text).Pad(cw))
	} else {
		body.Put(0, 0, ui.L(bg).B(core.Size(total), p.Text).Sp(1).T("on this computer", p.Muted).Pad(cw))
	}
	body.Put(0, 1, win.Text("Files you open are kept here, so they open fast.", p.Faint))
	y := 3
	nw := min(18, cw/3)
	for _, d := range drives {
		if y >= win.Room(maxH)-2 {
			break
		}
		size := core.Size(d.CacheBytes)
		bw := cw - nw - 2 - 10
		bar := ui.ThinBar(bw, float64(d.CacheBytes)/float64(max(total, 1)), st.T, p.Accent, bg)
		body.Put(0, y, ui.L(bg).Raw(ui.L(bg).T(core.Fit(d.Name, nw-1), p.Text).Pad(nw)).Raw(bar).Right(cw, ui.L(bg).T(size, p.Muted)))
		y++
	}
	if len(drives) > 0 {
		y++
	}
	body.Put(0, y, win.Text(core.Home(st.Snap.CacheDir), p.Faint))
	win.Fit(y+1, maxH)
	win.Body = body
	return win
}

func (s *storage) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	return s.window(st, maxW, maxH).Layer()
}

// enter names the focused button; c stays a shortcut for Clear
func (s *storage) Actions(st *core.State) []core.Action {
	if len(s.Buttons(st)) < 2 {
		return []core.Action{{Key: "enter", Label: "close"}, {Key: "esc", Label: "close"}}
	}
	enter := core.Action{Key: "enter", Label: "close"}
	if s.at == "btn:clear" {
		enter = core.Action{Key: "enter", Label: "clear", Danger: true}
	}
	return []core.Action{enter, {Key: "← →", Label: "choose"}, {Key: "c", Label: "clear", Danger: true}, {Key: "esc", Label: "close"}}
}

func (s *storage) Animating(*core.State) bool { return false }
