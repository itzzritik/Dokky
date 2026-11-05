package drives

import (
	"os"
	"strconv"
	"strings"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

func home(path string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, h) {
		return "~" + strings.TrimPrefix(path, h)
	}
	return path
}

func details(st *core.State, b *ui.Box, d api.Drive, x, w, h int) {
	p := ui.P()
	put := func(y int, s string) {
		if y < h {
			b.Put(x, y, s)
		}
	}
	put(0, ui.L(nil).B(d.Name, p.Text).Right(w, ui.DriveStatus(d, st.T).Line(nil, true)))
	kind := d.ServiceType
	for _, pr := range st.Catalog {
		if pr.Type == d.ServiceType {
			kind = pr.Name
		}
	}
	put(1, ui.Paint(kind, p.Muted, nil, false))
	switch sp := d.Space; {
	case sp != nil && sp.Total > 0:
		used := 1 - float64(sp.Free)/float64(sp.Total)
		var tone = p.Text
		var bar = ui.ThickBar(w, used, st.T, nil)
		if used > .9 {
			tone, bar = p.Buoy, ui.ThickBar(w, used, st.T, p.Buoy)
		}
		left := ui.L(nil).B(core.Size(sp.Free)+" free", tone).Sp(1).T("of "+core.Size(sp.Total), p.Muted)
		put(3, left.Right(w, ui.L(nil).T(strconv.Itoa(int(used*100+.5))+"% used", p.Faint)))
		put(4, bar)
	case d.State == api.Connected:
		put(3, ui.Paint("Free space unknown", p.Muted, nil, true))
		put(4, ui.Paint("This storage does not report it.", p.Faint, nil, false))
	default:
		put(3, ui.Paint("Free space unknown", p.Muted, nil, true))
		put(4, ui.Paint("Connect to see it.", p.Faint, nil, false))
	}
	at := "Stays off"
	if d.AtLogin {
		at = "Connects automatically"
	}
	cache := "Empty"
	if d.CacheBytes > 0 {
		cache = core.Size(d.CacheBytes)
	}
	facts := [][2]string{{"Account", d.Account}, {"Folder", home(d.Folder)}, {"At login", at}}
	if len(d.AlsoOn) > 0 {
		facts = append(facts, [2]string{"Also on", strings.Join(d.AlsoOn, ", ")})
	}
	if !d.AddedAt.IsZero() {
		facts = append(facts, [2]string{"Added", d.AddedAt.Local().Format("2 January 2006")})
	}
	facts = append(facts, [2]string{"Cache", cache})
	two := w >= 70
	colW, per := w, len(facts)
	if two {
		colW, per = w/2, (len(facts)+1)/2
	}
	for i, f := range facts {
		cx, cy := 0, 6+i
		if two {
			cx, cy = (i/per)*colW, 6+i%per
		}
		if cy < h {
			b.Put(x+cx, cy, ui.L(nil).Raw(ui.L(nil).T(f[0], p.Faint).Pad(11)).T(f[1], p.Text).Pad(colW-1))
		}
	}
	ry := 6 + per + 1
	put(ry, ui.Paint("Recent", p.Muted, nil, false))
	recent(st, b, d, x, ry+1, w, h)
}

func recent(st *core.State, b *ui.Box, d api.Drive, x, y, w, h int) {
	p := ui.P()
	n := 0
	row := func(icon, ic, name, what, when string) {
		if y+n >= h {
			return
		}
		c := p.Accent
		switch ic {
		case "ok":
			c = p.Kelp
		case "bad":
			c = p.Coral
		}
		nameW := max(10, (w-4)/3)
		b.Put(x, y+n, ui.L(nil).B(icon, c).Sp(1).Raw(ui.L(nil).T(name, p.Text).Pad(nameW)).Sp(1).T(what, p.Muted).Right(w, ui.L(nil).T(when, p.Faint)))
		n++
	}
	for _, t := range st.Act.Now {
		if t.DriveID != d.ID || t.Waiting {
			continue
		}
		pct := 0
		if t.Size > 0 {
			pct = int(float64(t.Done) / float64(t.Size) * 100)
		}
		icon, what := ui.G.Down, "Downloading, "
		if t.Up {
			icon, what = ui.G.Up, "Uploading, "
		}
		row(icon, "", t.Name, what+strconv.Itoa(pct)+"%", "now")
	}
	for _, t := range st.Act.Earlier {
		if t.DriveID != d.ID {
			continue
		}
		what := "Downloaded"
		if t.Up {
			what = "Uploaded"
		}
		if t.Error != "" {
			row(ui.G.Cross, "bad", t.Name, "Could not "+strings.ToLower(strings.TrimSuffix(what, "ed")), core.Ago(t.At, st.Now))
			continue
		}
		row(ui.G.Check, "ok", t.Name, what, core.Ago(t.At, st.Now))
	}
	if n == 0 && y < h {
		b.Put(x, y, ui.Paint("Nothing has moved yet.", p.Faint, nil, false))
	}
}
