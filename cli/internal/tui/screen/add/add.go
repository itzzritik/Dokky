package add

import (
	"strconv"
	"strings"
	"time"

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

const easeFor = 400 * time.Millisecond

// add a drive in four steps; edit reuses the sign-in and name steps
type Add struct {
	step step
	edit *api.Drive

	prov    api.Provider
	account *api.AccountRef
	pick    picker
	sign    signForm
	named   nameForm
	run     runner

	from, at float64
	movedAt  time.Time
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
	a.named.fill(st, d)
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

func (a *Add) goTo(s step, st *core.State) {
	a.from, a.movedAt = a.progress(st), st.Now
	a.step = s
}

func (a *Add) progress(st *core.State) float64 {
	if a.movedAt.IsZero() {
		return a.target()
	}
	return ui.Ease(a.from, a.target(), a.movedAt, st.Now, easeFor)
}

func (a *Add) target() float64 {
	if a.step == connect {
		return (3 + float64(a.run.checked())/3) / 4
	}
	return float64(a.step+1) / 4
}

func (a *Add) Capturing() bool { return true }

func (a *Add) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	if p, ok := msg.(core.Progress); ok && a.step == connect {
		before := a.run.checked()
		a.run.progress = api.AddProgress(p)
		if a.run.checked() != before {
			a.from, a.movedAt = a.progress(st), st.Now
		}
		return a, nil
	}
	if r, ok := msg.(core.Reply); ok && r.Req == a.pick.req {
		a.pick.req = 0
		if r.Err == nil {
			a.pick.accounts = r.Val.([]api.AccountRef)
		}
		return a, nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "esc" {
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
		a.goTo(name, st)
	case a.step == connect && a.run.done != nil:
		return core.Close(a)
	case a.step == connect:
		a.goTo(name, st)
	case a.step == signin && a.sign.working():
		a.sign.stop()
	case a.step == signin && a.edit != nil:
		a.goTo(name, st)
	case a.step == name && a.edit == nil && a.account == nil:
		a.goTo(signin, st)
	case a.step == choose || a.edit != nil:
		return core.Close(a)
	default:
		a.goTo(choose, st)
	}
	return nil
}

// what the window's buttons do; Enter and Esc reach the same places
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

// the bar for the current step; the primary one is what Enter does
func (a *Add) buttons() ([]ui.Btn, int) {
	back := ui.Btn{ID: "back", Label: "Back"}
	switch a.step {
	case choose:
		return []ui.Btn{{ID: "cancel", Label: "Cancel"}}, -1
	case signin:
		s := &a.sign
		switch {
		case s.working() && s.authURL != "":
			return []ui.Btn{{ID: "stop", Label: "Stop"}, {ID: "copy", Label: "Copy link"}, {ID: "open", Label: "Open again"}}, -1
		case s.working():
			return []ui.Btn{{ID: "stop", Label: "Stop"}}, -1
		case s.oauth && s.question == nil:
			return []ui.Btn{back, {ID: "next", Label: "Open browser", Primary: true}}, 1
		}
		if a.edit != nil {
			back.Label = "Cancel"
		}
		return []ui.Btn{back, {ID: "next", Label: "Continue", Primary: true}}, 1
	case name:
		if a.edit != nil {
			return []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "save", Label: "Save", Primary: true}}, 1
		}
		return []ui.Btn{back, {ID: "add", Label: "Add drive", Primary: true}}, 1
	}
	btns := a.connectButtons()
	switch {
	case a.run.working():
		return btns, -1
	case a.run.failed != "":
		return btns, 1
	}
	return btns, min(a.run.btn, len(btns)-1)
}

func (a *Add) choose(st *core.State, p api.Provider, acct *api.AccountRef) tea.Cmd {
	a.prov, a.account = p, acct
	if acct != nil {
		a.named.reset(unique(st, acct.Name))
		a.goTo(name, st)
		return nil
	}
	a.goTo(signin, st)
	return a.sign.load(st, p.Type, "")
}

func (a *Add) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	win := &ui.Window{W: min(76, maxW), Step: strconv.Itoa(int(a.step)+1) + " of 4", Progress: a.progress(st), Shimmer: st.T}
	if a.edit != nil {
		win.Step, win.Progress = "", 0
	}
	win.Buttons, win.Active = a.buttons()
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
		label := "add drive"
		if a.edit != nil {
			label = "save"
		}
		return []core.Action{{Key: "↑↓", Label: "move"}, {Key: "enter", Label: label}, {Key: "esc", Label: "back"}}
	}
	switch {
	case a.run.working():
		return []core.Action{{Key: "esc", Label: "cancel"}}
	case a.run.failed != "":
		return []core.Action{{Key: "enter", Label: "try again"}, {Key: "esc", Label: "back"}}
	}
	return []core.Action{{Key: "← →", Label: "choose"}, {Key: "enter", Label: "select"}}
}

func (a *Add) Animating(st *core.State) bool {
	if ui.Reduced() {
		return false
	}
	return ui.Easing(a.movedAt, st.Now, easeFor) || a.sign.working() || a.run.working() || a.pick.loading()
}
