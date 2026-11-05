package overview

import (
	"strconv"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

func activity(st *core.State, w int) string {
	p := ui.P()
	a := st.Act
	iw := w - 4
	var total []float64
	for _, s := range a.History {
		total = append(total, s.Down+s.Up)
	}
	body := ui.Graph(iw, 4, total, ui.Phase(a.History, st.Now))
	body = append(body, "")
	var moving, waiting []api.Transfer
	for _, t := range a.Now {
		if t.Waiting {
			waiting = append(waiting, t)
		} else {
			moving = append(moving, t)
		}
	}
	for _, t := range moving[:min(2, len(moving))] {
		body = append(body, transferLine(st, t, iw))
	}
	if len(moving) == 0 {
		body = append(body, ui.Paint("Nothing is moving.", p.Faint, nil, false))
	}
	if n := len(waiting) + max(0, len(moving)-2); n > 0 {
		body = append(body, ui.L(nil).Sp(2).T(strconv.Itoa(n)+" more waiting", p.Faint).String())
	}
	speeds := ui.G.Down + " " + core.Speed(a.Down) + "   " + ui.G.Up + " " + core.Speed(a.Up)
	return ui.Panel(w, panelH, "Activity", speeds, body)
}

func transferLine(st *core.State, t api.Transfer, w int) string {
	p := ui.P()
	dir, where := ui.G.Down, "from "
	if t.Up {
		dir, where = ui.G.Up, "to "
	}
	if d, ok := st.Drive(t.DriveID); ok {
		where += d.Name
	}
	frac := 0.0
	if t.Size > 0 {
		frac = float64(t.Done) / float64(t.Size)
	}
	nameW := max(8, (w-12)/3)
	barW := max(4, w-2-nameW-1-nameW-1-5)
	return ui.L(nil).B(dir, p.Accent).Sp(1).Raw(ui.L(nil).T(t.Name, p.Text).Pad(nameW)).Sp(1).
		Raw(ui.L(nil).T(where, p.Muted).Pad(nameW)).Sp(1).Raw(ui.ThinBar(barW, frac, st.T, nil)).
		T(pct(frac), p.Muted).String()
}

func pct(f float64) string {
	s := strconv.Itoa(int(f*100)) + "%"
	for len(s) < 5 {
		s = " " + s
	}
	return s
}

func (o *Overview) space(st *core.State, w int) string {
	p := ui.P()
	var body []string
	grow := ui.Ease(0, 1, o.shown, st.Now, growFor)
	for _, d := range st.Snap.Drives {
		if d.Space == nil || d.Space.Total <= 0 || len(body) >= 8 {
			continue
		}
		used := 1 - float64(d.Space.Free)/float64(d.Space.Total)
		tone, fc := p.Ramp[2], p.Muted
		if used > .9 {
			tone, fc = p.Buoy, p.Buoy
		}
		free := ui.L(nil).T(core.Size(d.Space.Free)+" free", fc)
		body = append(body, ui.L(nil).T(d.Name, p.Text).Right(w-4, free), ui.ThinBar(w-4, used*grow, st.T, tone))
	}
	if len(body) == 0 {
		msg := "Connect a drive to see its space."
		for _, d := range st.Snap.Drives {
			if d.State == api.Connected {
				msg = "No free space info yet."
			}
		}
		body = append(body, ui.Paint(msg, p.Faint, nil, false))
	}
	return ui.Panel(w, panelH, "Space", "", body)
}
