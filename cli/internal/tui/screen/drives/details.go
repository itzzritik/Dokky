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

func details(st *core.State, b *ui.Box, d api.Drive, x, w, h int, grow float64) {
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
	for i, l := range ui.SpaceMeter(w, d, st.T, grow, nil) {
		put(3+i, l)
	}
	at := "Stays off"
	if d.AtLogin {
		at = "Connects automatically"
	}
	cache := "Empty"
	if d.CacheBytes > 0 {
		cache = core.Size(d.CacheBytes)
	}
	added := ""
	if !d.AddedAt.IsZero() {
		added = d.AddedAt.Local().Format("2 January 2006")
	}
	// every drive shows the same facts in the same places
	facts := [][2]string{{"Account", d.Account}, {"Folder", home(d.Folder)}, {"At login", at},
		{"Added", added}, {"Cache", cache}, {"Also on", strings.Join(d.AlsoOn, ", ")}}
	two := w >= 70
	colW, per := w, len(facts)
	if two {
		colW, per = w/2, (len(facts)+1)/2
	}
	for i, f := range facts {
		cx, cy := 0, 7+i
		if two {
			cx, cy = (i/per)*colW, 7+i%per
		}
		v, vc := f[1], p.Text
		if v == "" {
			v, vc = ui.G.Dash, p.Faint
		}
		if cy < h {
			b.Put(x+cx, cy, ui.L(nil).Raw(ui.L(nil).T(f[0], p.Faint).Pad(11)).T(v, vc).Pad(colW-1))
		}
	}
	ry := 7 + per + 1
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
