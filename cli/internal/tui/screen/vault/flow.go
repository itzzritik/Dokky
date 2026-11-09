package vault

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/brand"
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
	exitQuit  exit = iota // a full screen: esc quits dokky
	exitClose             // a window that can be put away
	exitStay              // a step sign in can't finish without
)

// a vault screen: forms stacked in one window, as a modal or as a full screen under the dokky mark
type Flow struct {
	forms  []form
	window bool
	exit   exit
	signin bool // the master password step of a sign in
	mark   *brand.Mark
	done   func(*core.State) tea.Cmd
}

func newFlow(st *core.State, first form, window bool, ex exit) *Flow {
	f := &Flow{forms: []form{first}, window: window, exit: ex}
	if !window {
		f.mark = brand.NewMark(st.Now)
	}
	return f
}

func (f *Flow) top() form { return f.forms[len(f.forms)-1] }

func (f *Flow) push(fm form) { f.forms = append(f.forms, fm) }

// the showing form gives way to the next, like a new password to its recovery key
func (f *Flow) swap(fm form) {
	f.top().wipe()
	f.forms[len(f.forms)-1] = fm
}

// esc: one form back, else what the flow does on its first form
func (f *Flow) back() tea.Cmd {
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
	}
	return nil
}

// the flow's work is done; a window closes itself first
func (f *Flow) finish(st *core.State) tea.Cmd {
	f.wipe()
	var cmd tea.Cmd
	if f.done != nil {
		cmd = f.done(st)
	}
	if f.window {
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
			return f, f.back()
		}
	}
	return f, f.top().update(msg, f, st)
}

func (f *Flow) build(st *core.State, maxW, maxH int) *ui.Window {
	win := &ui.Window{W: min(64, maxW), Active: -1}
	rows := f.top().view(st, win, maxH)
	win.Fit(rows, maxH)
	return win
}

func (f *Flow) View(st *core.State, w, h int) *lipgloss.Layer {
	if f.window {
		return f.build(st, w, h).Layer()
	}
	return f.cover(st, w, h)
}

// the window centred under the dokky mark and clear of the footer; the mark becomes a word, then goes, when room is short
func (f *Flow) cover(st *core.State, w, h int) *lipgloss.Layer {
	p := ui.P()
	room := h - 3
	win := f.build(st, w-4, room)
	b := ui.NewBox()
	place := func(head int) int {
		free := room - head - win.H
		if free >= 2 {
			return max(1, free/2)
		}
		return max(0, free/2)
	}
	x := (w - win.W) / 2
	switch {
	case win.H+brand.H+2 <= room:
		y := place(brand.H + 2)
		b.Put((w-brand.W)/2, y, f.mark.View(st.Now))
		b.Add(x, y+brand.H+2, win.Layer())
	case win.H+2 <= room:
		y := place(2)
		b.Put((w-5)/2, y, ui.Paint("dokky", p.Accent, nil, true))
		b.Add(x, y+2, win.Layer())
	default:
		b.Add(x, place(0), win.Layer())
	}
	return b.Layer()
}

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
	case f.exit == exitClose:
		acts = append(acts, core.Action{Key: "esc", Label: "cancel"})
	}
	return acts
}

func (f *Flow) Animating(st *core.State) bool {
	return f.top().animating(st) || f.mark != nil && f.mark.Animating(st.Now)
}

// a vault step finished; the app checks again what covers the screen
type Settled struct{}

// the window lock was lifted
type Released struct{}
