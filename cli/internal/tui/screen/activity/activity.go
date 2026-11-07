package activity

import (
	"cmp"
	"image/color"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

const listW = 38

// every change on every device: a list on the left, the chosen entry on the right
type Activity struct {
	sel, top int
	selID    string
	open     bool
}

func New() *Activity { return &Activity{} }

func (s *Activity) current(st *core.State) (api.Event, bool) {
	items := st.Log.Items
	for i, e := range items {
		if e.ID == s.selID {
			s.sel = i
		}
	}
	if len(items) == 0 {
		return api.Event{}, false
	}
	s.sel = max(0, min(s.sel, len(items)-1))
	s.selID = items[s.sel].ID
	return items[s.sel], true
}

func (s *Activity) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	items := st.Log.Items
	switch m := msg.(type) {
	case core.Click:
		for i, e := range items {
			if m.ID == "row:"+e.ID {
				s.sel, s.selID, s.open = i, e.ID, !st.Wide
			}
		}
	case tea.KeyPressMsg:
		switch m.String() {
		case "up":
			s.move(items, -1)
		case "down":
			s.move(items, 1)
		case "enter":
			s.open = !st.Wide && len(items) > 0
		case "esc":
			s.open = false
		}
	}
	return s, nil
}

func (s *Activity) move(items []api.Event, by int) {
	if len(items) == 0 {
		return
	}
	s.sel = max(0, min(len(items)-1, s.sel+by))
	s.selID = items[s.sel].ID
}

func (s *Activity) Actions(st *core.State) []core.Action {
	if len(st.Log.Items) == 0 {
		return nil
	}
	if !st.Wide && s.open {
		return []core.Action{{Key: "esc", Label: "back"}}
	}
	acts := []core.Action{{Key: "↑↓", Label: "move"}}
	if !st.Wide {
		acts = append(acts, core.Action{Key: "enter", Label: "details"})
	}
	return acts
}

func (s *Activity) Animating(*core.State) bool { return false }

func device(e api.Event) string {
	return strings.TrimSuffix(cmp.Or(clean(e.Device), "This device"), ".local")
}

// text from other devices and error messages may carry newlines or escape codes
func clean(s string) string { return core.Safe(strings.ReplaceAll(s, "\n", " ")) }

// what happened, in a few words a person would say
func Summary(e api.Event) string {
	n := clean(e.Name)
	switch e.Kind {
	case "drive.added":
		return "Added " + n
	case "drive.renamed":
		return "Renamed " + n
	case "drive.folder":
		return "Changed the folder of " + n
	case "drive.cache":
		return "Changed the cache of " + n
	case "drive.mounted":
		return "Mounted " + n
	case "drive.unmounted":
		return "Unmounted " + n
	case "drive.mountFailed":
		return "Couldn't mount " + n
	case "drive.recovered":
		return n + " mounted again"
	case "drive.removed":
		return "Removed " + n
	case "drive.removedOn":
		return "Finished removing " + n
	case "signin.added":
		return "Signed in to " + n
	case "signin.fixed":
		return "Signed in again to " + n
	case "signin.expired":
		return "Sign-in expired for " + n
	case "device.added":
		return "Added device " + n
	case "device.removed":
		return "Removed device " + n
	case "device.erased":
		return n + " erased"
	case "device.renamed":
		return "Renamed a device to " + n
	}
	return n
}

func (s *Activity) View(st *core.State, w, h int) *lipgloss.Layer {
	p := ui.P()
	b := ui.NewBox()
	items := st.Log.Items
	if len(items) == 0 {
		b.Put(0, 0, ui.Paint("No activity yet", p.Text, nil, true))
		b.Put(0, 1, ui.Paint("Changes on any of your devices show here.", p.Muted, nil, false))
		return b.Layer()
	}
	e, _ := s.current(st)
	if !st.Wide && s.open {
		details(st, b, e, 0, w, h)
		return b.Layer()
	}
	lw := w
	if st.Wide {
		lw = listW
	}
	head := ui.L(nil).B("All activity", p.Text)
	if st.Log.Unseen > 0 {
		head.Sp(1).Raw(ui.Badge(strconv.Itoa(st.Log.Unseen), nil))
	}
	b.Put(2, 0, head.String())
	b.Put(2, 1, ui.Paint(strings.Repeat(ui.G.H, max(0, lw-3)), ui.Mix(p.Bg, p.Text, .12), nil, false))
	room := h - 3
	if s.sel < s.top {
		s.top = s.sel
	}
	if s.sel >= s.top+room {
		s.top = s.sel - room + 1
	}
	for i := s.top; i < len(items) && i < s.top+room; i++ {
		x, on := items[i], i == s.sel
		b.Add(0, 3+i-s.top, ui.Row("row:"+x.ID, lw, on, nil, func(bg color.Color, iw int) string {
			when := ui.L(bg).T(core.Ago(x.At.Local(), st.Now), p.Faint)
			fg := ui.Mix(p.Muted, p.Text, .55)
			if !x.OK {
				fg = p.Coral
			} else if on {
				fg = p.Text
			}
			return ui.L(bg).T(core.Fit(Summary(x), iw-when.Width()-2), fg).Right(iw, when)
		}))
	}
	if st.Wide {
		rule := make([]string, h)
		for i := range rule {
			rule[i] = ui.Paint(ui.G.V, ui.Mix(p.Bg, p.Text, .12), nil, false)
		}
		b.Lines(listW+3, 0, rule)
		details(st, b, e, listW+7, w-listW-7, h)
	}
	return b.Layer()
}

func details(st *core.State, b *ui.Box, e api.Event, x, w, h int) {
	p := ui.P()
	put := func(y int, s string) {
		if y < h {
			b.Put(x, y, s)
		}
	}
	mark, tone := ui.G.Check, p.Kelp
	if !e.OK {
		mark, tone = ui.G.Cross, p.Coral
	}
	put(0, ui.L(nil).B(mark, tone).Sp(1).B(core.Fit(Summary(e), w-2), p.Text).String())
	put(1, ui.Paint(core.Ago(e.At.Local(), st.Now), p.Muted, nil, false))
	what := "Drive"
	switch {
	case strings.HasPrefix(e.Kind, "signin."):
		what = "Account"
	case strings.HasPrefix(e.Kind, "device."):
		what = "Device name"
	}
	facts := [][2]string{{"When", e.At.Local().Format("2 January 2006, 3:04pm")}, {"On", device(e)}, {what, clean(e.Name)}}
	if e.Reason != "" {
		label := "Note"
		if !e.OK {
			label = "Why"
		}
		facts = append(facts, [2]string{label, clean(e.Reason)})
	}
	y := 3
	for _, f := range facts {
		put(y, ui.Paint(f[0], p.Faint, nil, false))
		for _, line := range wrap(f[1], w) {
			y++
			put(y, ui.Paint(line, p.Text, nil, false))
		}
		y += 2
	}
}

// long reasons break onto more lines instead of running off the edge
func wrap(s string, w int) []string {
	w = max(10, w)
	var out []string
	for len([]rune(s)) > w {
		r := []rune(s)
		cut := w
		for i := w; i > w/2; i-- {
			if r[i] == ' ' {
				cut = i
				break
			}
		}
		out = append(out, strings.TrimSpace(string(r[:cut])))
		s = strings.TrimSpace(string(r[cut:]))
	}
	return append(out, s)
}
