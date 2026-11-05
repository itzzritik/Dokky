package activity

import (
	"context"
	"image/color"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

const graphRows = 5

type Activity struct{ sel, top int }

func New() *Activity { return &Activity{} }

type entry struct {
	t       api.Transfer
	earlier bool
}

func entries(st *core.State) (now, earlier []entry) {
	for _, t := range st.Act.Now {
		if !t.Waiting {
			now = append(now, entry{t: t})
		}
	}
	for _, t := range st.Act.Now {
		if t.Waiting {
			now = append(now, entry{t: t})
		}
	}
	for _, t := range st.Act.Earlier {
		earlier = append(earlier, entry{t: t, earlier: true})
	}
	return now, earlier
}

func (s *Activity) current(st *core.State) (entry, bool) {
	now, earlier := entries(st)
	all := append(now, earlier...)
	if len(all) == 0 {
		return entry{}, false
	}
	s.sel = max(0, min(s.sel, len(all)-1))
	return all[s.sel], true
}

func retryable(e entry) bool { return e.t.Up && (e.t.Waiting || e.t.Error != "") }

func (s *Activity) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	now, earlier := entries(st)
	n := len(now) + len(earlier)
	switch m := msg.(type) {
	case core.Click:
		if i, err := strconv.Atoi(strings.TrimPrefix(m.ID, "row:")); err == nil && i < n {
			s.sel = i
		}
	case tea.KeyPressMsg:
		switch m.String() {
		case "up":
			s.sel = max(0, s.sel-1)
		case "down":
			s.sel = max(0, min(n-1, s.sel+1))
		case "enter":
			e, ok := s.current(st)
			if !ok {
				return s, nil
			}
			if retryable(e) {
				c, id, name := st.C, e.t.DriveID, e.t.Name
				return s, func() tea.Msg {
					if err := c.Retry(context.Background(), id, name); err != nil {
						return core.Toast{Text: core.Safe(err.Error()), Bad: true}
					}
					return core.Toast{Text: "Trying " + core.Safe(name) + " again"}
				}
			}
			if d, ok := st.Drive(e.t.DriveID); ok && e.earlier && d.State == api.Connected {
				return s, core.OpenFolder(filepath.Join(d.Folder, filepath.FromSlash(path.Dir(e.t.Name))))
			}
		}
	}
	return s, nil
}

func (s *Activity) Actions(st *core.State) []core.Action {
	acts := []core.Action{{Key: "↑↓", Label: "move"}}
	e, ok := s.current(st)
	switch {
	case !ok:
	case retryable(e):
		acts = append(acts, core.Action{Key: "enter", Label: "try now"})
	case e.earlier:
		if d, ok := st.Drive(e.t.DriveID); ok && d.State == api.Connected {
			acts = append(acts, core.Action{Key: "enter", Label: "show in " + core.FileManager()})
		}
	}
	return acts
}

func (s *Activity) Animating(st *core.State) bool {
	return ui.Showing(st.Act.History)
}

func (s *Activity) View(st *core.State, w, h int) *lipgloss.Layer {
	p := ui.P()
	b := ui.NewBox()
	a := st.Act
	now, earlier := entries(st)
	moving, waiting := 0, 0
	for _, e := range now {
		if e.t.Waiting {
			waiting++
		} else {
			moving++
		}
	}
	speeds := ui.L(nil).B(ui.G.Down, p.Accent).Sp(1).B(core.Speed(a.Down), p.Text).Sp(4).B(ui.G.Up, p.Accent).Sp(1).B(core.Speed(a.Up), p.Text)
	b.Put(2, 0, speeds.Right(w-2, ui.L(nil).T(strconv.Itoa(moving)+" moving, "+strconv.Itoa(waiting)+" waiting", p.Muted)))
	var total []float64
	for _, x := range a.History {
		total = append(total, x.Down+x.Up)
	}
	b.Lines(2, 2, ui.Graph(w-3, graphRows, total, ui.Phase(a.History, st.Now)))

	var lines []ui.ListLine
	head := func(t string, c color.Color) ui.ListLine {
		return ui.ListLine{Text: ui.L(nil).Sp(2).T(t, c).String(), Item: -1}
	}
	lines = append(lines, head("Now", p.Muted))
	for i, e := range now {
		lines = append(lines, ui.ListLine{Text: s.row(st, e, i, w), Item: i, ID: "row:" + strconv.Itoa(i)})
	}
	if len(now) == 0 {
		lines = append(lines, head("Nothing is moving.", p.Faint))
	}
	lines = append(lines, ui.ListLine{Item: -1}, head("Earlier", p.Muted))
	for i, e := range earlier {
		lines = append(lines, ui.ListLine{Text: s.row(st, e, len(now)+i, w), Item: len(now) + i, ID: "row:" + strconv.Itoa(len(now)+i)})
	}
	if len(earlier) == 0 {
		lines = append(lines, head("Nothing has moved yet.", p.Faint))
	}
	ui.List(b, graphRows+3, h-(graphRows+3), &s.top, s.sel, lines)
	return b.Layer()
}

func (s *Activity) row(st *core.State, e entry, i, w int) string {
	p := ui.P()
	t := e.t
	drive := ""
	if d, ok := st.Drive(t.DriveID); ok {
		drive = d.Name
	}
	nameW, driveW := max(12, w/4), max(10, w/6)
	return ui.RowLine(w, i == s.sel, nil, func(bg color.Color, iw int) string {
		l := ui.L(bg)
		var right *ui.Line
		switch {
		case !e.earlier && t.Waiting:
			l.B(ui.G.Dash, p.Faint).Sp(1).Raw(ui.L(bg).T(t.Name, p.Muted).Pad(nameW)).Sp(1).Raw(ui.L(bg).T(drive, p.Muted).Pad(driveW))
			when := "Waiting"
			if d := t.RetryAt.Sub(st.Now); d > 0 {
				when = "Waiting, " + core.In(d)
			}
			right = ui.L(bg).T(when, p.Faint)
		case !e.earlier:
			dir := ui.G.Down
			if t.Up {
				dir = ui.G.Up
			}
			frac := 0.0
			if t.Size > 0 {
				frac = float64(t.Done) / float64(t.Size)
			}
			barW := max(6, iw-2-nameW-1-driveW-1-5-14)
			l.B(dir, p.Accent).Sp(1).Raw(ui.L(bg).T(t.Name, p.Text).Pad(nameW)).Sp(1).Raw(ui.L(bg).T(drive, p.Muted).Pad(driveW)).Sp(1).
				Raw(ui.ThinBar(barW, frac, st.T, nil)).T(pctOf(frac), p.Muted)
			left := "Almost done"
			if t.ETA > 0 {
				left = core.In(t.ETA) + " left"
			}
			right = ui.L(bg).T(left, p.Faint)
		case t.Error != "":
			l.B(ui.G.Cross, p.Coral).Sp(1).Raw(ui.L(bg).T(t.Name, p.Text).Pad(nameW)).Sp(1).Raw(ui.L(bg).T(drive, p.Muted).Pad(driveW)).Sp(1).T(core.Size(t.Size), p.Faint)
			what := "Could not upload: "
			if !t.Up {
				what = "Could not download: "
			}
			reason, _, _ := strings.Cut(t.Error, "\n")
			what += reason
			if d := t.RetryAt.Sub(st.Now); d > 0 {
				what += ". Trying again in " + core.In(d)
			}
			right = ui.L(bg).T(core.Fit(what, iw/2), p.Coral)
		default:
			l.B(ui.G.Check, p.Kelp).Sp(1).Raw(ui.L(bg).T(t.Name, p.Text).Pad(nameW)).Sp(1).Raw(ui.L(bg).T(drive, p.Muted).Pad(driveW)).Sp(1).T(core.Size(t.Size), p.Faint)
			what := "Downloaded "
			if t.Up {
				what = "Uploaded "
			}
			right = ui.L(bg).T(what+core.Ago(t.At, st.Now), p.Faint)
		}
		return l.Right(iw, right)
	})
}

func pctOf(f float64) string {
	s := strconv.Itoa(int(f*100)) + "%"
	for len(s) < 5 {
		s = " " + s
	}
	return s
}
