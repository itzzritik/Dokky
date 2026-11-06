package overview

import (
	"path"
	"strconv"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// panel width from which the files sit beside the graph instead of under it
const splitW = 90

// the graph and its scale, with the files moving now and just moved beside it (under it when narrow)
func activity(st *core.State, w, h int) string {
	p := ui.P()
	a := st.Act
	iw, ih := w-4, h-2
	var total []float64
	peak := 0.0
	for _, s := range a.History {
		total = append(total, s.Down+s.Up)
		peak = max(peak, s.Down+s.Up)
	}
	split := w >= splitW
	gw, gh, fw := iw, 4, iw
	if ih >= 12 {
		gh = 5
	}
	fh := ih - gh - 1
	if split {
		gw = (iw - 3) / 2
		gh, fw, fh = ih-1, iw-3-gw, ih
	}
	graph := append(ui.Graph(gw, gh, total, ui.Phase(a.History, st.Now)), axis(gw, peak))
	list := files(st, fw, fh)
	body := append(graph, list...)
	if split {
		sep := ui.Paint(ui.G.V, p.Line, nil, false)
		body = make([]string, ih)
		for i := range body {
			body[i] = ui.L(nil).Raw(ui.L(nil).Raw(graph[i]).Pad(gw)).Sp(1).Raw(sep).Sp(1).Raw(list[i]).String()
		}
	}
	speeds := ui.G.Down + " " + core.Speed(a.Down) + "   " + ui.G.Up + " " + core.Speed(a.Up)
	return ui.Panel(w, h, "Transfers", speeds, body)
}

// "2 min ago", the peak, "now" under the graph
func axis(w int, peak float64) string {
	p := ui.P()
	left, right := "2 min ago", "now"
	l := ui.L(nil).T(left, p.Faint)
	if mid := "peak " + core.Speed(peak); peak > 0 {
		if gap := (w-ui.Width(mid))/2 - ui.Width(left); gap >= 2 && gap+ui.Width(left)+ui.Width(mid)+2+ui.Width(right) <= w {
			l.Sp(gap).T(mid, p.Muted)
		}
	}
	return l.Right(w, ui.L(nil).T(right, p.Faint))
}

type file struct {
	t    api.Transfer
	done bool
}

// moving first, then waiting, then the latest finished; exactly n lines
func files(st *core.State, w, n int) []string {
	p := ui.P()
	out := make([]string, max(0, n))
	if n <= 0 {
		return out
	}
	var all []file
	for _, waiting := range []bool{false, true} {
		for _, t := range st.Act.Now {
			if t.Waiting == waiting {
				all = append(all, file{t: t})
			}
		}
	}
	for _, t := range st.Act.Earlier {
		all = append(all, file{t: t, done: true})
	}
	if len(all) == 0 {
		out[0] = ui.Paint("Nothing has moved yet.", p.Faint, nil, false)
		return out
	}
	show := len(all)
	if show > n {
		show = n - 1
		out[n-1] = ui.Paint(strconv.Itoa(len(all)-show)+" more in Transfers", p.Faint, nil, false)
	}
	for i, f := range all[:show] {
		out[i] = fileLine(st, f, w)
	}
	return out
}

func fileLine(st *core.State, f file, w int) string {
	p := ui.P()
	t := f.t
	l, right := ui.L(nil), ui.L(nil)
	nc := p.Text
	switch {
	case !f.done && t.Waiting:
		l.B(ui.G.Dash, p.Faint)
		nc = p.Muted
		when := "Waiting"
		if d := t.RetryAt.Sub(st.Now); d > 0 {
			when += ", " + core.In(d)
		}
		right.T(when, p.Faint)
	case !f.done:
		dir := ui.G.Down
		if t.Up {
			dir = ui.G.Up
		}
		l.B(dir, p.Accent)
		frac := 0.0
		if t.Size > 0 {
			frac = float64(t.Done) / float64(t.Size)
		}
		right.Raw(ui.ThinBar(max(6, w/5), frac, st.T, nil, nil)).T(pct(frac), p.Muted)
	case t.Error != "":
		l.B(ui.G.Cross, p.Coral)
		right.T("Failed", p.Coral)
	default:
		l.B(ui.G.Check, p.Kelp)
		right.T(core.Ago(t.At, st.Now), p.Faint)
	}
	l.Sp(1).T(path.Base(t.Name), nc)
	if d, ok := st.Drive(t.DriveID); ok {
		l.Sp(2).T(d.Name, p.Faint)
	}
	return l.Right(w, right)
}

func pct(f float64) string {
	s := strconv.Itoa(int(f*100)) + "%"
	for len(s) < 5 {
		s = " " + s
	}
	return s
}
