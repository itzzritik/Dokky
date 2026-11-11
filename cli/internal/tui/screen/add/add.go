package add

import (
	"slices"
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

func newAdd() *Add {
	a := &Add{}
	a.pick.search = ui.NewField("", "", "Search all services", false)
	a.pick.at = searchID
	return a
}

func Open(st *core.State, typ string) tea.Cmd {
	a := newAdd()
	cmd := a.pick.load(st)
	if typ != "" {
		if p, ok := provider(st, typ); ok {
			cmd = tea.Batch(cmd, a.choose(st, p, nil))
		}
	}
	return tea.Sequence(core.Open(a), cmd)
}

func Edit(st *core.State, d api.Drive) tea.Cmd { return core.Open(editing(st, d)) }

// straight to signing in again, for a drive whose login failed; esc lands on its edit window
func SignIn(st *core.State, d api.Drive) tea.Cmd {
	a := editing(st, d)
	return tea.Sequence(core.Open(a), a.toSignIn(st))
}

func editing(st *core.State, d api.Drive) *Add {
	a := &Add{edit: &d, step: name, ed: editForm{saving: -1, saved: -1, at: rowID(eName)}}
	a.prov, _ = provider(st, d.ServiceType)
	if a.prov.Type == "" {
		a.prov = api.Provider{Type: d.ServiceType, Name: d.ServiceName}
	}
	return a
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
	out := []ui.Step{{Label: "Service"}, {Label: "Sign in"}, {Label: "Details"}, {Label: "Mount"}}
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

// the edit window of a drive already added, not a wizard step
func (a *Add) editingDrive() bool { return a.edit != nil && a.step == name }

// keys are text only while a field has focus, so ? works everywhere else
func (a *Add) Capturing() bool {
	switch {
	case a.editingDrive():
		return a.ed.field != nil
	case a.step == signin && a.sign.working():
		return false
	}
	return strings.HasPrefix(*a.atp(), "field:")
}

func (a *Add) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	cmd := a.update(msg, st)
	// focus stays on a stop that is still there, so the view and the footer agree on it
	*a.atp() = a.focused(st)
	return a, cmd
}

func (a *Add) update(msg tea.Msg, st *core.State) tea.Cmd {
	if p, ok := msg.(core.Progress); ok && a.step == connect {
		a.run.progress = api.AddProgress(p)
		return nil
	}
	if r, ok := msg.(core.Reply); ok && r.Req == a.pick.req {
		a.pick.req = 0
		if r.Err == nil {
			a.pick.accounts = r.Val.([]api.AccountRef)
		}
		return nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "esc" {
		switch {
		// a typed search clears first, then a second esc closes
		case a.step == choose && a.pick.search.Value() != "":
			a.pick.clear()
			return nil
		// an open value or list closes before the window does
		case a.editingDrive() && (a.ed.field != nil || a.ed.menu):
		default:
			return a.back(st)
		}
	}
	if c, ok := msg.(core.Click); ok && ui.IsBtn(c.ID) {
		if a.editingDrive() {
			a.ed.close()
		}
		*a.atp() = c.ID
		return a.press(strings.TrimPrefix(c.ID, "btn:"), st)
	}
	switch a.step {
	case choose:
		return a.updateChoose(msg, st)
	case signin:
		return a.updateSign(msg, st)
	case name:
		return a.updateName(msg, st)
	}
	return a.updateConnect(msg, st)
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
	case a.step == signin && a.edit != nil:
		a.sign.stop()
		a.goTo(name)
	// a new drive's sign-in belongs to its service, so going back to Service drops it, even one waiting in the browser
	case a.step == signin:
		a.sign.stop()
		a.sign, a.account = signForm{}, nil
		a.toChoose()
	// details go back one step, keeping a finished sign-in so it is not asked again
	case a.step == name && a.edit == nil && a.account == nil:
		a.goTo(signin)
	case a.step == name && a.edit == nil:
		a.toChoose()
	case a.step == choose || a.edit != nil:
		return core.Close(a)
	}
	return nil
}

func (a *Add) toChoose() {
	a.pick.at = searchID
	a.goTo(choose)
}

// what each button does, pressed by a click, enter or space
func (a *Add) press(id string, st *core.State) tea.Cmd {
	switch id {
	case "cancel", "back":
		return a.back(st)
	case "next":
		if a.step == choose {
			return a.pick.chosen(a, st)
		}
		return a.signNext(st)
	case "copy", "open":
		return a.sign.copyOrOpen(id)
	case "add":
		if a.step == name && a.edit == nil {
			return a.submitName(st)
		}
	case "retry":
		if a.run.failed != "" {
			return a.run.start(st, a.run.params)
		}
	case "done":
		return a.finish(false)
	case "finder":
		return a.finish(true)
	case "mount", "view", "signin", "remove":
		return a.editPress(id, st)
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
	a.named = nameForm{}
	a.goTo(signin)
	return a.sign.load(st, p.Type, "")
}

// the focused stop of each step lives with that step's form
func (a *Add) atp() *string {
	switch {
	case a.step == choose:
		return &a.pick.at
	case a.step == signin:
		return &a.sign.at
	case a.editingDrive():
		return &a.ed.at
	case a.step == name:
		return &a.named.at
	}
	return &a.run.at
}

// the stops above the bar, in reading order
func (a *Add) above() []string {
	switch {
	case a.step == choose:
		return []string{searchID}
	case a.step == signin:
		return a.sign.stops()
	case a.editingDrive():
		return editStops()
	case a.step == name:
		return nameStops
	}
	return nil
}

// every stop tab walks: fields, chips and rows, then the buttons
func (a *Add) stops(st *core.State) []string {
	out := slices.Clone(a.above())
	for _, b := range a.bar(st) {
		out = append(out, "btn:"+b.ID)
	}
	return out
}

// the buttons along the bottom; the primary is the one enter reaches from a field
func (a *Add) bar(st *core.State) []ui.Btn {
	switch {
	case a.step == choose:
		return []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "next", Label: "Next", Primary: true}}
	case a.step == signin:
		return a.signBar(st)
	case a.editingDrive():
		return a.editBar(st)
	case a.step == name:
		return []ui.Btn{{ID: "back", Label: "Back"}, {ID: "add", Label: "Add drive", Primary: true}}
	}
	return a.connectBar(st)
}

// what has focus: the stop last moved to while it is still there, else where the step starts
func (a *Add) focused(st *core.State) string {
	at, above, bar := *a.atp(), a.above(), a.bar(st)
	if slices.Contains(above, at) || slices.ContainsFunc(bar, func(b ui.Btn) bool { return "btn:"+b.ID == at }) {
		return at
	}
	if len(above) > 0 && !ui.IsBtn(at) {
		return above[0]
	}
	// a button that went away hands focus to the primary, never to a danger button
	if i := slices.IndexFunc(bar, func(b ui.Btn) bool { return b.Primary }); i >= 0 {
		return "btn:" + bar[i].ID
	}
	for i := len(bar) - 1; i >= 0; i-- {
		if !bar[i].Danger {
			return "btn:" + bar[i].ID
		}
	}
	if len(above) > 0 {
		return above[0]
	}
	return ""
}

// loading, checking or mounting: only esc and clicks act until it answers
func (a *Add) busy() bool {
	return a.step == signin && a.sign.working() && !a.sign.waiting() || a.step == connect && a.run.working()
}

func (a *Add) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	return a.window(st, maxW, maxH).Layer()
}

func (a *Add) window(st *core.State, maxW, maxH int) *ui.Window {
	at := a.focused(st)
	win := &ui.Window{W: min(76, maxW), Buttons: a.bar(st), Active: -1, Hints: ui.WindowKeys(a.Actions(st))}
	if a.edit == nil {
		win.Steps = a.steps()
	}
	primary := slices.IndexFunc(win.Buttons, func(b ui.Btn) bool { return b.Primary })
	switch i := win.Index(at); {
	case a.busy():
		win.Active = primary
	case i >= 0:
		win.Active = i
	// enter on a field ends at the primary; a row in the edit list acts on itself
	case !a.editingDrive():
		win.Active = primary
	}
	var rows int
	switch a.step {
	case choose:
		rows = a.viewChoose(st, win, maxH, at)
	case signin:
		rows = a.viewSign(st, win, maxH, at)
	case name:
		rows = a.viewName(st, win, maxH, at)
	default:
		rows = a.viewConnect(st, win)
	}
	win.Fit(rows, maxH)
	return win
}

var moveAction = core.Action{Key: "↑↓", Label: "move"}

func lowerFirst(s string) string { return strings.ToLower(s[:1]) + s[1:] }

func upperFirst(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

// a focused button: enter does what it says, left and right walk the bar, tab goes back up
func (a *Add) pressing(st *core.State, at string) []core.Action {
	bar := a.bar(st)
	i := slices.IndexFunc(bar, func(b ui.Btn) bool { return "btn:"+b.ID == at })
	if i < 0 {
		return nil
	}
	var acts []core.Action
	// a mount already under way has nothing to press
	if !a.editingDrive() || bar[i].ID != "mount" || core.MountLabel(a.live(st)) != "" {
		acts = append(acts, core.Action{Key: "enter", Label: lowerFirst(bar[i].Label), Danger: bar[i].Danger})
	}
	if len(bar) > 1 {
		acts = append(acts, core.Action{Key: "← →", Label: "switch"})
	}
	if len(a.above()) > 0 {
		acts = append(acts, moveAction)
	}
	return acts
}

func (a *Add) Actions(st *core.State) []core.Action {
	at := a.focused(st)
	if a.editingDrive() {
		return a.editActions(st, at)
	}
	esc := core.Action{Key: "esc", Label: a.escLabel()}
	if a.busy() {
		return []core.Action{esc}
	}
	var acts []core.Action
	switch {
	case ui.IsBtn(at):
		acts = a.pressing(st, at)
	case a.step == choose:
		acts = []core.Action{{Key: "↑↓", Label: "move"}, {Key: "enter", Label: "choose"}}
	case strings.HasPrefix(at, "chip:"):
		acts = []core.Action{a.enterOnField(st, at), {Key: "← →", Label: "choose"}, moveAction}
	default:
		acts = []core.Action{a.enterOnField(st, at), moveAction}
	}
	return append(acts, esc)
}

// enter on a field goes to the next one still needed, else does what the primary says
func (a *Add) enterOnField(st *core.State, at string) core.Action {
	next := core.Action{Key: "enter", Label: "next"}
	switch {
	case a.step == signin && a.sign.nextEmpty(fieldIndex(at)) >= 0:
		return next
	case a.step == name && a.named.needsName(at):
		return next
	}
	for _, b := range a.bar(st) {
		if b.Primary {
			return core.Action{Key: "enter", Label: lowerFirst(b.Label)}
		}
	}
	return next
}

func (a *Add) escLabel() string {
	switch {
	case a.step == choose && a.pick.search.Value() != "":
		return "clear"
	case a.step == choose:
		return "close"
	case a.step == signin && a.sign.waiting(), a.step == connect && a.run.working():
		return "cancel"
	case a.step == connect && a.run.done != nil:
		return "done"
	}
	return "back"
}

func (a *Add) Animating(st *core.State) bool {
	if ui.Reduced() {
		return false
	}
	return a.sign.working() || a.run.working() || a.pick.loading() || a.editingDrive() && (a.ed.animating(st.Now) || core.MountLabel(a.live(st)) == "")
}
