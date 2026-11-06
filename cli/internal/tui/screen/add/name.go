package add

import (
	"runtime"
	"strconv"
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
	focus        int
	err          string
}

const (
	fName = iota
	fFolder
	fLogin
)

func (n *nameForm) reset(name string) {
	*n = nameForm{name: ui.NewField("Name", name, "", false), folder: ui.NewField("Folder", "", "Whole account", false)}
}

func (a *Add) nameItems() []int { return []int{fName, fFolder, fLogin} }

// cloud sign-ins go through the browser; their details are not edited here
func oauthType(t string) bool {
	switch t {
	case "drive", "onedrive", "dropbox", "pcloud", "box", "googlephotos", "yandex", "jottacloud", "hidrive", "zoho", "putio", "premiumizeme", "sharefile", "pikpak":
		return true
	}
	return false
}

func (n *nameForm) input(item int) *ui.Field {
	return map[int]*ui.Field{fName: n.name, fFolder: n.folder}[item]
}

func (a *Add) updateName(msg tea.Msg, st *core.State) tea.Cmd {
	if a.edit != nil {
		return a.updateEdit(msg, st)
	}
	n := &a.named
	items := a.nameItems()
	n.focus = min(n.focus, len(items)-1)
	item := items[n.focus]
	switch m := msg.(type) {
	case core.Click:
		if i, err := strconv.Atoi(strings.TrimPrefix(m.ID, "item:")); err == nil {
			for k, it := range items {
				if it == i {
					n.focus = k
				}
			}
		}
		if k, ok := strings.CutPrefix(m.ID, "chip:login:"); ok {
			n.focus, n.stayOff = 2, int(k[0]-'0')
		}
		return nil
	case tea.KeyPressMsg:
		switch m.String() {
		case "down":
			n.focus = min(len(items)-1, n.focus+1)
			return nil
		case "up":
			n.focus = max(0, n.focus-1)
			return nil
		case "enter":
			return a.submitName(st)
		case "left", "right", "space":
			if item == fLogin {
				n.stayOff = 1 - n.stayOff
				return nil
			}
		}
	}
	if f := n.input(item); f != nil {
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
		n.focus, n.err = 0, "Give the drive a name."
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

func (a *Add) viewName(st *core.State, win *ui.Window, maxH int) int {
	if a.edit != nil {
		return a.viewEdit(st, win)
	}
	p := ui.P()
	n := &a.named
	cw := win.Inner()
	win.Title, win.Sub = "Drive details", "Set its name, folder and what happens at login."
	if n.err != "" {
		win.Sub, win.SubBad = n.err, true
	}
	cur := a.nameItems()[min(n.focus, 2)]
	body := ui.NewBox()
	body.Add(0, 0, n.name.View("item:"+strconv.Itoa(fName), cw, cur == fName, p.Surface))
	body.Add(0, 2, n.folder.View("item:"+strconv.Itoa(fFolder), cw, cur == fFolder, p.Surface))
	name := cmpOr(strings.TrimSpace(n.name.Value()), "…")
	body.Put(0, 4, ui.L(p.Surface).T("In "+core.FileManager(), p.Muted).Pad(ui.LabelW)+ui.L(p.Surface).Sp(2).B(finderPath(engine.Title(name)), p.Accent).Pad(cw-ui.LabelW))
	body.Put(0, 6, ui.L(p.Surface).T("At login", p.Muted).Pad(ui.LabelW))
	body.Add(ui.LabelW, 6, ui.Chips("chip:login", atLogin, n.stayOff, cur == fLogin, p.Surface))
	win.Body = body
	return min(win.Room(maxH), 7)
}
