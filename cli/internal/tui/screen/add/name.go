package add

import (
	"runtime"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

var atLogin = []string{"Mount", "Stay off"}

type nameForm struct {
	name, folder *ui.Field
	stayOff      int
	at           string // the focused stop
	err          string
}

// the name, the folder, then the at login chips
var nameStops = []string{"field:0", "field:1", "chip:login"}

func (n *nameForm) reset(name string) {
	*n = nameForm{name: ui.NewField("Name", name, "", false), folder: ui.NewField("Folder", "", "Whole account", false)}
}

// the name is the only thing asked for, so enter anywhere else goes there first while it is empty
func (n *nameForm) needsName(at string) bool {
	return at != "field:0" && strings.TrimSpace(n.name.Value()) == ""
}

// cloud sign-ins go through the browser; their details are not edited here
func oauthType(t string) bool {
	switch t {
	case "drive", "onedrive", "dropbox", "pcloud", "box", "googlephotos", "yandex", "jottacloud", "hidrive", "zoho", "putio", "premiumizeme", "sharefile", "pikpak":
		return true
	}
	return false
}

func (n *nameForm) input(at string) *ui.Field {
	switch at {
	case "field:0":
		return n.name
	case "field:1":
		return n.folder
	}
	return nil
}

func (a *Add) updateName(msg tea.Msg, st *core.State) tea.Cmd {
	if a.edit != nil {
		return a.updateEdit(msg, st)
	}
	n := &a.named
	n.at = a.focused(st)
	switch m := msg.(type) {
	case core.Click:
		if slices.Contains(nameStops, m.ID) {
			n.at = m.ID
		}
		if k, ok := strings.CutPrefix(m.ID, "chip:login:"); ok && k != "" {
			n.at, n.stayOff = "chip:login", int(k[0]-'0')
		}
		return nil
	case tea.KeyPressMsg:
		switch k := m.String(); {
		case ui.Pressed(k, n.at):
			return a.press(strings.TrimPrefix(n.at, "btn:"), st)
		case k == "enter" && n.needsName(n.at):
			n.at = "field:0"
			return nil
		case k == "enter":
			return a.submitName(st)
		case n.at == "chip:login" && slide(&n.stayOff, len(atLogin), k):
			return nil
		case ui.Walk(a.stops(st), &n.at, k):
			return nil
		}
	}
	if f := n.input(n.at); f != nil {
		cmd, typed := f.Type(msg)
		if typed {
			n.err = ""
		}
		return cmd
	}
	return nil
}

func (a *Add) toSignIn(st *core.State) tea.Cmd {
	a.goTo(signin)
	return a.sign.load(st, a.edit.ServiceType, a.edit.ServiceID)
}

func (a *Add) submitName(st *core.State) tea.Cmd {
	n := &a.named
	name := strings.TrimSpace(n.name.Value())
	if name == "" {
		n.at, n.err = "field:0", "Give the drive a name."
		return nil
	}
	sub := strings.Trim(strings.TrimSpace(n.folder.Value()), "/")
	nd := api.NewDrive{Name: name, SubPath: sub, AtLogin: n.stayOff == 0}
	if a.account != nil {
		nd.ServiceID = a.account.ID
	} else {
		nd.Service = &api.ServiceDraft{Type: a.prov.Type, Name: name, Options: a.sign.draft}
	}
	a.goTo(connect)
	return a.run.start(st, nd)
}

func finderPath(name string) string {
	if runtime.GOOS == "darwin" {
		return "Dokky › " + name
	}
	return "~/dokky/" + name
}

func (a *Add) viewName(st *core.State, win *ui.Window, maxH int, at string) int {
	if a.edit != nil {
		return a.viewEdit(st, win, at)
	}
	p := ui.P()
	n := &a.named
	cw := win.Inner()
	win.Title, win.Sub = "Drive details", "Set its name, folder and what happens at login."
	if n.err != "" {
		win.Sub, win.SubBad = n.err, true
	}
	body := ui.NewBox()
	body.Add(0, 0, n.name.View("field:0", cw, at == "field:0", p.Window))
	body.Add(0, 2, n.folder.View("field:1", cw, at == "field:1", p.Window))
	name := cmpOr(strings.TrimSpace(n.name.Value()), "…")
	body.Put(0, 4, ui.L(p.Window).T("In "+core.FileManager(), p.Muted).Pad(ui.LabelW)+ui.L(p.Window).Sp(2).B(finderPath(engine.Title(name)), p.Accent).Pad(cw-ui.LabelW))
	body.Put(0, 6, ui.L(p.Window).T("At login", p.Muted).Pad(ui.LabelW))
	body.Add(ui.LabelW, 6, ui.Chips("chip:login", atLogin, n.stayOff, at == "chip:login", p.Window))
	win.Body = body
	return min(win.Room(maxH), 7)
}
