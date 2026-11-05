package drives

import (
	"image/color"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/add"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

const listW = 34

type Drives struct {
	sel, top int
	open     bool
	find     *ui.Field
	finding  bool
	selID    string
	meterID  string
	meterAt  time.Time
}

func New() *Drives { return &Drives{find: ui.NewField("", "", "Find a drive", false)} }

func (s *Drives) list(st *core.State) []api.Drive {
	q := strings.ToLower(strings.TrimSpace(s.find.Value()))
	var out []api.Drive
	for _, d := range st.Snap.Drives {
		if q == "" || strings.Contains(strings.ToLower(d.Name), q) {
			out = append(out, d)
		}
	}
	return out
}

// selection follows the drive, not the row, as the list changes
func (s *Drives) current(st *core.State) (api.Drive, bool) {
	list := s.list(st)
	for i, d := range list {
		if d.ID == s.selID {
			s.sel = i
		}
	}
	if len(list) == 0 {
		return api.Drive{}, false
	}
	s.sel = max(0, min(s.sel, len(list)-1))
	s.selID = list[s.sel].ID
	return list[s.sel], true
}

func (s *Drives) Capturing() bool { return s.finding }

func (s *Drives) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	d, ok := s.current(st)
	n := len(s.list(st))
	switch m := msg.(type) {
	case core.SwitchTab:
		if m.Tab == core.DrivesTab {
			s.meterID = ""
		}
		if m.Drive != "" {
			s.finding = false
			s.find.Input.SetValue("")
			for i, x := range s.list(st) {
				if x.ID == m.Drive {
					s.sel, s.selID, s.open = i, x.ID, !st.Wide
				}
			}
		}
	case core.Click:
		for i, x := range s.list(st) {
			if m.ID == "row:"+x.ID {
				s.sel, s.selID = i, x.ID
				s.open = !st.Wide
			}
		}
	case tea.KeyPressMsg:
		k := m.String()
		if s.finding {
			switch k {
			case "esc":
				s.finding = false
				s.find.Input.SetValue("")
				return s, nil
			case "enter":
				s.finding = false
				return s, nil
			case "up", "down":
			default:
				var cmd tea.Cmd
				s.find.Input, cmd = s.find.Input.Update(m)
				s.sel = 0
				return s, cmd
			}
		}
		switch k {
		case "up":
			s.move(st, -1, n)
		case "down":
			s.move(st, 1, n)
		case "/":
			s.finding, s.open = true, false
		case "a":
			return s, add.Open(st, "")
		case "esc":
			s.open = false
		}
		if !ok {
			return s, nil
		}
		switch k {
		case "enter":
			if !st.Wide && !s.open {
				s.open = true
				return s, nil
			}
			return s, add.Edit(st, d)
		case "o":
			return s, core.RunPrimary(st, d)
		}
	}
	return s, nil
}

func (s *Drives) move(st *core.State, by, n int) {
	if n == 0 {
		return
	}
	s.sel = max(0, min(n-1, s.sel+by))
	s.selID = s.list(st)[s.sel].ID
}

func (s *Drives) Actions(st *core.State) []core.Action {
	d, ok := s.current(st)
	if s.finding {
		return []core.Action{{Key: "↑↓", Label: "move"}, {Key: "enter", Label: "done"}, {Key: "esc", Label: "clear"}}
	}
	if !ok {
		return []core.Action{{Key: "a", Label: "add"}}
	}
	if !st.Wide && !s.open {
		return []core.Action{{Key: "↑↓", Label: "move"}, {Key: "enter", Label: "details"}, {Key: "a", Label: "add"}, {Key: "/", Label: "find"}}
	}
	acts := []core.Action{{Key: "↑↓", Label: "move"}}
	if s.open && !st.Wide {
		acts = []core.Action{{Key: "esc", Label: "back"}}
	}
	acts = append(acts, core.Action{Key: "enter", Label: "edit"})
	if p := core.Primary(d); p != "" {
		// a drive that isn't mounted: connecting is the next thing to do
		acts = append(acts, core.Action{Key: "o", Label: p, Accent: p == "connect"})
	}
	if st.Wide {
		acts = append(acts, core.Action{Key: "a", Label: "add"}, core.Action{Key: "/", Label: "find"})
	}
	return acts
}

func (s *Drives) Animating(st *core.State) bool {
	if ui.Reduced() {
		return false
	}
	for _, d := range st.Snap.Drives {
		if d.State == api.Connecting {
			return true
		}
	}
	d, ok := s.current(st)
	if !ok || !st.Wide && !s.open {
		return false
	}
	return ui.MeterGrowing(s.meterAt, st.Now) || ui.MeterMoving() && d.Space != nil && d.Space.Total > d.Space.Free
}

func (s *Drives) View(st *core.State, w, h int) *lipgloss.Layer {
	p := ui.P()
	b := ui.NewBox()
	if len(st.Snap.Drives) == 0 {
		b.Put(0, 0, ui.Paint("No drives yet", p.Text, nil, true))
		b.Put(0, 1, ui.Paint("Add one to see it here.", p.Muted, nil, false))
		return b.Layer()
	}
	d, ok := s.current(st)
	if ok && d.ID != s.meterID {
		s.meterID, s.meterAt = d.ID, st.Now
	}
	grow := ui.MeterGrow(s.meterAt, st.Now)
	if !st.Wide && s.open && ok {
		details(st, b, d, 0, w, h, grow)
		return b.Layer()
	}
	lw := w
	if st.Wide {
		lw = listW
	}
	list := s.list(st)
	if s.finding || s.find.Value() != "" {
		b.Add(0, 0, s.find.View("find", lw, s.finding, nil))
	} else {
		b.Put(2, 0, ui.L(nil).B("All drives", p.Text).Right(lw-3, ui.L(nil).Raw(ui.Badge(strconv.Itoa(len(st.Snap.Drives)), nil))))
	}
	b.Put(2, 1, ui.Paint(strings.Repeat(ui.G.H, max(0, lw-3)), ui.Mix(p.Bg, p.Text, .12), nil, false))
	room := h - 3
	if s.sel < s.top {
		s.top = s.sel
	}
	if s.sel >= s.top+room {
		s.top = s.sel - room + 1
	}
	for i := s.top; i < len(list) && i < s.top+room; i++ {
		x, on := list[i], i == s.sel
		b.Add(0, 3+i-s.top, ui.Row("row:"+x.ID, lw, on, nil, func(bg color.Color, iw int) string {
			nc := ui.Mix(p.Muted, p.Text, .55)
			l := ui.L(bg)
			if on {
				l.B(x.Name, p.Text)
			} else {
				l.T(x.Name, nc)
			}
			return l.Right(iw, ui.DriveStatus(x, st.T).Line(bg, !st.Wide))
		}))
	}
	if len(list) == 0 {
		b.Put(2, 3, ui.Paint("No drive matches.", p.Faint, nil, false))
	}
	if st.Wide {
		rule := make([]string, h)
		for i := range rule {
			rule[i] = ui.Paint(ui.G.V, ui.Mix(p.Bg, p.Text, .12), nil, false)
		}
		b.Lines(listW+3, 0, rule)
		if ok {
			details(st, b, d, listW+7, w-listW-7, h, grow)
		}
	}
	return b.Layer()
}
