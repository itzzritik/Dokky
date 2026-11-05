package add

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

type step int

const (
	choose step = iota
	signin
	name
	connect
)

// add a drive in four steps; edit reuses the sign-in and name steps
type Add struct {
	step step
	edit *api.Drive

	prov    api.Provider
	account *api.AccountRef
	pick    picker
	sign    signForm
	named   nameForm
	ed      editForm
	run     runner
}

func Open(st *core.State, typ string) tea.Cmd {
	a := &Add{}
	a.pick.search = ui.NewField("", "", "Search all services", false)
	cmd := a.pick.load(st)
	if typ != "" {
		if p, ok := provider(st, typ); ok {
			cmd = tea.Batch(cmd, a.choose(st, p, nil))
		}
	}
	return tea.Sequence(core.Open(a), cmd)
}

func Edit(st *core.State, d api.Drive) tea.Cmd {
	a := &Add{edit: &d, step: name}
	a.prov, _ = provider(st, d.ServiceType)
	if a.prov.Type == "" {
		a.prov = api.Provider{Type: d.ServiceType, Name: d.ServiceName}
	}
	return core.Open(a)
}

// "Home NAS", then "Home NAS 2" when a drive already has that folder name
func unique(st *core.State, name string) string {
	taken := func(n string) bool {
		for _, d := range st.Snap.Drives {
			if strings.EqualFold(d.Name, n) {
				return true
			}
		}
		return false
	}
	out := name
	for i := 2; taken(out); i++ {
		out = name + " " + strconv.Itoa(i)
	}
	return out
}

func provider(st *core.State, typ string) (api.Provider, bool) {
	for _, p := range st.Catalog {
		if p.Type == typ {
			return p, true
		}
	}
	return api.Provider{}, false
}

func (a *Add) goTo(s step) { a.step = s }

// Service, Sign in, Details, Connect; sign-in is skipped when an account already signed in was picked
func (a *Add) steps() []ui.Step {
	out := []ui.Step{{Label: "Service"}, {Label: "Sign in"}, {Label: "Details"}, {Label: "Connect"}}
	for i := range out {
		switch s := step(i); {
		case s == signin && a.account != nil && a.step > choose:
			out[i].State = ui.StepSkipped
		case s < a.step || s == connect && a.run.done != nil:
			out[i].State = ui.StepDone
		case s == a.step:
			out[i].State = ui.StepNow
		}
	}
	return out
}

func (a *Add) Capturing() bool { return true }

func (a *Add) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	if p, ok := msg.(core.Progress); ok && a.step == connect {
		a.run.progress = api.AddProgress(p)
		return a, nil
	}
	if r, ok := msg.(core.Reply); ok && r.Req == a.pick.req {
		a.pick.req = 0
		if r.Err == nil {
			a.pick.accounts = r.Val.([]api.AccountRef)
		}
		return a, nil
	}
	// esc while typing a value only drops that edit
	if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "esc" && (a.edit == nil || a.step != name || a.ed.field == nil) {
		return a, a.back(st)
	}
	if c, ok := msg.(core.Click); ok {
		if id, ok := strings.CutPrefix(c.ID, "btn:"); ok {
			return a, a.press(id, st)
		}
	}
	switch a.step {
	case choose:
		return a, a.updateChoose(msg, st)
	case signin:
		return a, a.updateSign(msg, st)
	case name:
		return a, a.updateName(msg, st)
	}
	return a, a.updateConnect(msg, st)
}

func (a *Add) back(st *core.State) tea.Cmd {
	switch {
	case a.step == connect && a.run.working():
		a.run.stop()
		a.goTo(name)
	case a.step == connect && a.run.done != nil:
		return core.Close(a)
	case a.step == connect:
		a.goTo(name)
	case a.step == signin && a.sign.working():
		a.sign.stop()
	case a.step == signin && a.edit != nil:
		a.goTo(name)
	// signed in already, so going back starts over instead of landing on a finished sign-in
	case a.step == name && a.edit == nil:
		a.sign.stop()
		a.sign, a.account = signForm{}, nil
		a.goTo(choose)
	case a.step == choose || a.edit != nil:
		return core.Close(a)
	default:
		a.goTo(choose)
	}
	return nil
}

// what each step's keys and clicks do
func (a *Add) press(id string, st *core.State) tea.Cmd {
	switch id {
	case "cancel", "back", "stop":
		return a.back(st)
	case "next":
		return a.sign.submit(st)
	case "copy":
		return a.sign.copyOrOpen("c")
	case "open":
		return a.sign.copyOrOpen("o")
	case "add", "save":
		return a.submitName(st)
	case "retry":
		return a.run.start(st, a.run.params)
	case "done":
		return a.finish(false)
	case "finder":
		return a.finish(true)
	}
	return nil
}

func (a *Add) choose(st *core.State, p api.Provider, acct *api.AccountRef) tea.Cmd {
	a.prov, a.account = p, acct
	if acct != nil {
		a.named.reset(unique(st, acct.Name))
		a.goTo(name)
		return nil
	}
	a.goTo(signin)
	return a.sign.load(st, p.Type, "")
}

func (a *Add) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	win := &ui.Window{W: min(76, maxW)}
	if a.edit == nil {
		win.Steps = a.steps()
	}
	var rows int
	switch a.step {
	case choose:
		rows = a.viewChoose(st, win, maxH)
	case signin:
		rows = a.viewSign(st, win, maxH)
	case name:
		rows = a.viewName(st, win, maxH)
	default:
		rows = a.viewConnect(st, win)
	}
	win.Fit(rows, maxH)
	return win.Layer()
}

func (a *Add) Actions(st *core.State) []core.Action {
	switch a.step {
	case choose:
		return []core.Action{{Key: "↑↓", Label: "move"}, {Key: "enter", Label: "choose"}, {Key: "esc", Label: "close"}}
	case signin:
		if a.sign.working() && a.sign.authURL != "" {
			return []core.Action{{Key: "c", Label: "copy link"}, {Key: "o", Label: "open again"}, {Key: "esc", Label: "stop"}}
		}
		if a.sign.working() {
			return []core.Action{{Key: "esc", Label: "stop"}}
		}
		if a.sign.oauth && a.sign.question == nil {
			return []core.Action{{Key: "enter", Label: "open browser"}, {Key: "esc", Label: "back"}}
		}
		return []core.Action{{Key: "↑↓", Label: "move"}, {Key: "enter", Label: "continue"}, {Key: "esc", Label: "back"}}
	case name:
		if a.edit != nil {
			return a.editActions(st)
		}
		return []core.Action{{Key: "↑↓", Label: "move"}, {Key: "enter", Label: "add drive"}, {Key: "esc", Label: "back"}}
	}
	switch r := a.run; {
	case r.working():
		return []core.Action{{Key: "esc", Label: "cancel"}}
	case r.failed != "":
		return []core.Action{{Key: "enter", Label: "try again"}, {Key: "esc", Label: "back"}}
	case r.done != nil && r.done.State == api.Connected:
		return []core.Action{{Key: "enter", Label: "open in " + core.FileManager()}, {Key: "esc", Label: "done"}}
	}
	return []core.Action{{Key: "enter", Label: "done"}}
}

func (a *Add) Animating(st *core.State) bool {
	if ui.Reduced() {
		return false
	}
	return a.sign.working() || a.run.working() || a.pick.loading()
}
