package vault

import (
	"context"
	"slices"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/contour"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// one step of a vault screen, drawn into the flow's window
type form interface {
	update(msg tea.Msg, f *Flow, st *core.State) tea.Cmd
	view(st *core.State, win *ui.Window, maxH int) int // fills the window, returns its body rows
	actions(st *core.State) []core.Action
	typing() bool  // a field has focus, so q is a letter
	holding() bool // mid call or showing a recovery key: the app keeps this screen up
	animating(st *core.State) bool
	wipe()
}

// what esc does on the first form
type exit int

const (
	exitQuit   exit = iota // a full screen: esc quits dokky
	exitClose              // a window that can be put away
	exitStay               // a step that can't be left
	exitSignIn             // the sign in's master password step: esc cancels the sign in
)

// a vault screen: forms stacked in one window, as a modal or as a full screen under the dokky mark
type Flow struct {
	forms  []form
	window bool
	exit   exit
	signin bool // the master password step of a sign in
	stage  *ui.Stage
	done   func(*core.State) tea.Cmd
}

func newFlow(st *core.State, first form, window bool, ex exit) *Flow {
	f := &Flow{forms: []form{first}, window: window, exit: ex}
	if !window {
		f.stage = NewStage()
	}
	return f
}

func (f *Flow) top() form { return f.forms[len(f.forms)-1] }

// a form that acts as soon as it shows, like the lock screen asking for Touch ID
type starter interface{ start(*core.State) tea.Cmd }

// Start runs the first form's opening move, if it has one
func (f *Flow) Start(st *core.State) tea.Cmd {
	if s, ok := f.top().(starter); ok {
		return s.start(st)
	}
	return nil
}

func (f *Flow) push(fm form) { f.forms = append(f.forms, fm) }

// the showing form gives way to the next, like a new password to its recovery key
func (f *Flow) swap(fm form) {
	f.top().wipe()
	f.forms[len(f.forms)-1] = fm
}

// esc: one form back, else what the flow does on its first form
func (f *Flow) back(st *core.State) tea.Cmd {
	if len(f.forms) > 1 {
		f.top().wipe()
		f.forms = f.forms[:len(f.forms)-1]
		return nil
	}
	switch f.exit {
	case exitQuit:
		f.wipe()
		return core.Send(core.Quit{})
	case exitClose:
		f.wipe()
		return core.Close(f)
	case exitSignIn:
		f.wipe()
		return tea.Batch(core.Close(f), cancelSignIn(st))
	}
	return nil
}

// the daemon drops the waiting sign in, changing nothing on this computer, and the app goes back to where it was
func cancelSignIn(st *core.State) tea.Cmd {
	c := st.C
	return func() tea.Msg {
		if err := c.LoginCancel(context.Background()); err != nil {
			return core.Toast{Text: core.Report(err), Bad: true, Now: true}
		}
		return core.Toast{Text: "Sign in cancelled", Now: true}
	}
}

// the flow's work is done; a window closes itself first
func (f *Flow) finish(st *core.State) tea.Cmd {
	f.wipe()
	var cmd tea.Cmd
	if f.done != nil {
		cmd = f.done(st)
	}
	// a window, or the sign in's step opened like one, closes itself
	if f.window || f.signin {
		return tea.Batch(core.Close(f), cmd)
	}
	return tea.Batch(cmd, core.Send(Settled{}))
}

func (f *Flow) wipe() {
	for _, fm := range f.forms {
		fm.wipe()
	}
}

// the app keeps a full screen flow while it is mid call or showing a key, even when the vault's state moved on
func (f *Flow) Holding() bool { return f.top().holding() }

// the master password step of a sign in, so the app opens only one
func (f *Flow) SignInStep() bool { return f.signin }

// a full screen flow opened as a window, like the sign in's master password step, covers everything
func (f *Flow) Full() bool { return !f.window }

// every key comes here; q quits only where it isn't typed
func (f *Flow) Capturing() bool { return true }

func (f *Flow) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch m := msg.(type) {
	case core.Locked:
		f.wipe()
		if f.window && f.exit == exitClose {
			return f, core.Close(f)
		}
		return f, nil
	case tea.KeyPressMsg:
		if m.String() == "q" && !f.top().typing() && !f.top().holding() && len(f.forms) == 1 && f.exit == exitQuit {
			return f, f.back(st)
		}
	}
	return f, f.top().update(msg, f, st)
}

func (f *Flow) build(st *core.State, maxW, maxH int, hints []core.Action) *ui.Window {
	win := &ui.Window{W: min(64, maxW), Active: -1, Hints: hints}
	rows := f.top().view(st, win, maxH)
	win.Fit(rows, maxH)
	return win
}

func (f *Flow) View(st *core.State, w, h int) *lipgloss.Layer {
	if f.window {
		return f.build(st, w, h, ui.WindowKeys(f.Actions(st))).Layer()
	}
	return f.cover(st, w, h)
}

// Forged's gate: the card under the wordmark, the contour map below, and the keys inside the card since there is no footer
func (f *Flow) cover(st *core.State, w, h int) *lipgloss.Layer {
	return f.stage.Layer(w, h, st.Now, func(maxH int) *ui.Window {
		return f.build(st, ui.CardWidth(w), maxH, ui.WindowKeys(f.Actions(st)))
	})
}

// NewStage is the gate every full screen shares, the breathing contour map along its bottom
func NewStage() *ui.Stage { return &ui.Stage{New: contour.NewBand} }

func (f *Flow) Actions(st *core.State) []core.Action {
	acts := f.top().actions(st)
	if slices.ContainsFunc(acts, func(a core.Action) bool { return a.Key == "esc" }) || f.top().holding() {
		return acts
	}
	switch {
	case len(f.forms) > 1:
		acts = append(acts, core.Action{Key: "esc", Label: "back"})
	case f.exit == exitQuit:
		acts = append(acts, core.Action{Key: "esc", Label: "quit"})
	case f.exit == exitClose || f.exit == exitSignIn:
		acts = append(acts, core.Action{Key: "esc", Label: "cancel"})
	}
	return acts
}

func (f *Flow) Animating(st *core.State) bool {
	return f.top().animating(st) || f.stage != nil && f.stage.Moving()
}

// a vault step finished; the app checks again what covers the screen
type Settled struct{}

// the window lock was lifted
type Released struct{}
