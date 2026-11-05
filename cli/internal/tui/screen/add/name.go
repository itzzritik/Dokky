package add

import (
	"context"
	"image/color"
	"runtime"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

const advLabel = 26

var atLogin = []string{"Connect", "Stay off"}

type nameForm struct {
	name, folder          *ui.Field
	local, size, age, pin *ui.Field
	stayOff               int
	focus                 int
	adv                   bool
	err                   string
	req                   core.Req
}

// focus order; advanced items only in edit mode
const (
	fName = iota
	fFolder
	fLogin
	fAdv
	fLocal
	fSize
	fAge
	fPin
	fSignIn
	fConn
	fRemove
)

func (n *nameForm) reset(name string) {
	*n = nameForm{name: ui.NewField("Name", name, "", false), folder: ui.NewField("Folder", "", "Whole account", false)}
}

func (n *nameForm) fill(st *core.State, d api.Drive) {
	n.reset(d.Name)
	n.folder.Input.SetValue(d.SubPath)
	if !d.AtLogin {
		n.stayOff = 1
	}
	adv := func(label, v, hint string) *ui.Field { return ui.NewField(label, v, hint, false) }
	n.local = adv("Folder on this computer", d.LocalFolder, d.Folder)
	n.size = adv("Space for cached files", d.CacheMaxSize, "Like 10G")
	n.age = adv("Keep cached files for", d.CacheMaxAge, "Like 1h")
	n.pin = adv("Pinned version", st.Snap.Settings.PinnedVersion, "Latest")
}

func (a *Add) nameItems(st *core.State) []int {
	items := []int{fName, fFolder, fLogin}
	if a.edit == nil {
		return items
	}
	items = append(items, fAdv)
	if a.named.adv {
		items = append(items, fLocal, fSize, fAge, fPin)
	}
	if a.edit.ServiceType != "" && !oauthType(a.edit.ServiceType) {
		items = append(items, fSignIn)
	}
	if a.connLabel(st) != "" {
		items = append(items, fConn)
	}
	return append(items, fRemove)
}

// what the connection row does now: the drive's live state, not the one the window opened with
func (a *Add) connLabel(st *core.State) string {
	d, _ := st.Drive(a.edit.ID)
	switch d.State {
	case api.Connected:
		return "Disconnect"
	case api.Off, api.Failed:
		return "Connect"
	}
	return ""
}

func (a *Add) runConn(st *core.State) tea.Cmd {
	switch a.connLabel(st) {
	case "Disconnect":
		return tea.Batch(core.Close(a), core.Disconnect(st, a.edit.ID))
	case "Connect":
		return tea.Batch(core.Close(a), core.Connect(st, a.edit.ID))
	}
	return nil
}

func (a *Add) remove() tea.Cmd {
	d := *a.edit
	c := &confirm.Confirm{
		Question: "Remove " + core.Safe(d.Name) + "?",
		Line:     "Its folder leaves this computer. Your files stay.",
		Do:       "Remove",
		Run: func(st *core.State, forget bool) tea.Cmd {
			cl, id, name := st.C, d.ID, core.Safe(d.Name)
			return tea.Batch(core.Close(a), func() tea.Msg {
				if err := cl.RemoveDrive(context.Background(), api.DriveRemove{ID: id, ForgetAccount: forget}); err != nil {
					return core.Toast{Text: core.Report(err), Bad: true}
				}
				return core.Toast{Text: name + " removed"}
			})
		},
	}
	if d.AccountDrives == 1 {
		c.Toggle = "Also forget the sign-in"
	}
	return confirm.Open(c)
}

// cloud sign-ins go through the browser; their details are not edited here
func oauthType(t string) bool {
	switch t {
	case "drive", "onedrive", "dropbox", "pcloud", "box", "googlephotos", "yandex", "jottacloud", "hidrive", "zoho", "putio", "premiumizeme", "sharefile", "pikpak":
		return true
	}
	return false
}

func (n *nameForm) input(item int) *ui.Field {
	return map[int]*ui.Field{fName: n.name, fFolder: n.folder, fLocal: n.local, fSize: n.size, fAge: n.age, fPin: n.pin}[item]
}

func (a *Add) updateName(msg tea.Msg, st *core.State) tea.Cmd {
	n := &a.named
	items := a.nameItems(st)
	n.focus = min(n.focus, len(items)-1)
	item := items[n.focus]
	switch m := msg.(type) {
	case core.Reply:
		if m.Req != n.req {
			return nil
		}
		n.req = 0
		if m.Err != nil {
			n.err = core.Report(m.Err)
			return nil
		}
		return tea.Batch(core.Close(a), core.Say("Saved"))
	case core.Click:
		if i, err := strconv.Atoi(strings.TrimPrefix(m.ID, "item:")); err == nil {
			for k, it := range items {
				if it == i {
					n.focus = k
				}
			}
			switch i {
			case fAdv:
				n.adv = !n.adv
			case fSignIn:
				return a.toSignIn(st)
			case fConn:
				return a.runConn(st)
			case fRemove:
				return a.remove()
			}
		}
		if k, ok := strings.CutPrefix(m.ID, "chip:login:"); ok {
			n.focus, n.stayOff = 2, int(k[0]-'0')
		}
		return nil
	case tea.KeyPressMsg:
		if n.req != 0 {
			return nil
		}
		switch m.String() {
		case "down":
			n.focus = min(len(items)-1, n.focus+1)
			return nil
		case "up":
			n.focus = max(0, n.focus-1)
			return nil
		case "enter":
			switch item {
			case fAdv:
				n.adv = !n.adv
				return nil
			case fSignIn:
				return a.toSignIn(st)
			case fConn:
				return a.runConn(st)
			case fRemove:
				return a.remove()
			}
			return a.submitName(st)
		case "left", "right", "space":
			if item == fLogin {
				n.stayOff = 1 - n.stayOff
				return nil
			}
		}
	}
	if f := n.input(item); f != nil && n.req == 0 {
		cmd, typed := f.Type(msg)
		if typed {
			n.err = ""
		}
		return cmd
	}
	return nil
}

func (a *Add) toSignIn(st *core.State) tea.Cmd {
	a.goTo(signin, st)
	return a.sign.load(st, a.edit.ServiceType, a.edit.ServiceID)
}

func (a *Add) submitName(st *core.State) tea.Cmd {
	n := &a.named
	name := strings.TrimSpace(n.name.Value())
	if name == "" {
		n.focus, n.err = 0, "Give the drive a name."
		return nil
	}
	sub := strings.Trim(strings.TrimSpace(n.folder.Value()), "/")
	if a.edit == nil {
		nd := api.NewDrive{Name: name, SubPath: sub, AtLogin: n.stayOff == 0}
		if a.account != nil {
			nd.ServiceID = a.account.ID
		} else {
			nd.Service = &api.ServiceDraft{Type: a.prov.Type, Name: name, Options: a.sign.draft}
		}
		a.goTo(connect, st)
		return a.run.start(st, nd)
	}
	d, settings := *a.edit, st.Snap.Settings
	u := api.DriveUpdate{ID: d.ID}
	if name != d.Name {
		u.Name = name
	}
	if sub != d.SubPath {
		u.SubPath = &sub
	}
	if on := n.stayOff == 0; on != d.AtLogin {
		u.AtLogin = &on
	}
	if v := strings.TrimSpace(n.local.Value()); v != d.LocalFolder {
		u.LocalFolder = &v
	}
	if v := strings.TrimSpace(n.size.Value()); v != d.CacheMaxSize {
		u.CacheMaxSize = v
	}
	if v := strings.TrimSpace(n.age.Value()); v != d.CacheMaxAge {
		u.CacheMaxAge = v
	}
	pin := strings.TrimSpace(n.pin.Value())
	n.req = core.NextReq()
	return core.Do(st, n.req, func(ctx context.Context, c *client.Client) (any, error) {
		if _, err := c.UpdateDrive(ctx, u); err != nil {
			return nil, err
		}
		if pin != settings.PinnedVersion {
			settings.PinnedVersion = pin
			return nil, c.SetSettings(ctx, settings)
		}
		return nil, nil
	})
}

func finderPath(name string) string {
	if runtime.GOOS == "darwin" {
		return "Dokky › " + name
	}
	return "~/dokky/" + name
}

func (a *Add) viewName(st *core.State, win *ui.Window, maxH int) int {
	p := ui.P()
	n := &a.named
	cw := win.Inner()
	win.Title, win.Sub = "Name this drive", "This is the name you see in "+core.FileManager()+"."
	if a.edit != nil {
		win.Title, win.Sub = "Edit "+a.edit.Name, ""
		if k := a.edit.AccountDrives; k > 1 {
			win.Sub = "Also used by " + strconv.Itoa(k-1) + " more drives."
		}
	}
	switch {
	case n.req != 0:
		win.Sub = ui.Spin(st.T) + " Saving"
	case n.err != "":
		win.Sub, win.SubBad = n.err, true
	}
	items := a.nameItems(st)
	cur := items[min(n.focus, len(items)-1)]
	type piece struct {
		y    int
		item int
		put  func(b *ui.Box, y int)
	}
	var pieces []piece
	field := func(y, item int, f *ui.Field, lw int) {
		pieces = append(pieces, piece{y, item, func(b *ui.Box, y int) {
			if lw == 0 {
				b.Add(0, y, f.View("item:"+strconv.Itoa(item), cw, cur == item && n.req == 0, p.Surface))
				return
			}
			f2 := *f
			f2.Label = ""
			b.Put(0, y, ui.L(p.Surface).T(f.Label, p.Muted).Pad(lw))
			b.Add(lw, y, f2.View("item:"+strconv.Itoa(item), cw-lw, cur == item && n.req == 0, p.Surface))
		}})
	}
	row := func(y, item int, label, glyph string, fg color.Color) {
		pieces = append(pieces, piece{y, item, func(b *ui.Box, y int) {
			b.Add(0, y, ui.Row("item:"+strconv.Itoa(item), cw, cur == item, p.Surface, func(bg color.Color, iw int) string {
				return ui.L(bg).T(label, fg).Right(iw, ui.L(bg).T(glyph, p.Muted))
			}))
		}})
	}
	field(0, fName, n.name, 0)
	field(2, fFolder, n.folder, 0)
	name := cmpOr(strings.TrimSpace(n.name.Value()), "…")
	pieces = append(pieces, piece{4, -1, func(b *ui.Box, y int) {
		b.Put(0, y, ui.L(p.Surface).T("In "+core.FileManager(), p.Muted).Pad(ui.LabelW)+ui.L(p.Surface).Sp(2).B(finderPath(name), p.Accent).Pad(cw-ui.LabelW))
	}}, piece{6, fLogin, func(b *ui.Box, y int) {
		b.Put(0, y, ui.L(p.Surface).T("At login", p.Muted).Pad(ui.LabelW))
		b.Add(ui.LabelW, y, ui.Chips("chip:login", atLogin, n.stayOff, cur == fLogin, p.Surface))
	}})
	y := 6
	if a.edit != nil {
		y += 2
		caret := ui.G.Arrow
		if n.adv {
			caret = ui.G.Caret
		}
		row(y, fAdv, "Advanced", caret, p.Muted)
		if n.adv {
			for i, it := range []int{fLocal, fSize, fAge, fPin} {
				field(y+2+2*i, it, n.input(it), advLabel)
			}
			y += 8
		}
		if a.edit.ServiceType != "" && !oauthType(a.edit.ServiceType) {
			y += 2
			row(y, fSignIn, "Sign-in details", ui.G.Arrow, p.Muted)
		}
		if label := a.connLabel(st); label != "" {
			y += 2
			row(y, fConn, label, "", p.Muted)
		}
		y += 2
		row(y, fRemove, "Remove drive", "", p.Coral)
	}
	// the body scrolls so the focused item stays in the window
	room := win.Room(maxH)
	top := 0
	for _, pc := range pieces {
		if pc.item == cur && pc.y >= room {
			top = pc.y - room + 1
		}
	}
	body := ui.NewBox()
	for _, pc := range pieces {
		if pc.y >= top && pc.y-top < room {
			pc.put(body, pc.y-top)
		}
	}
	win.Body = body
	return min(room, y+1)
}
