package app

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/stretchr/testify/require"
)

// the messages a command sends, through batches and sequences, for commands that only send messages
func menuSent(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice {
		var out []tea.Msg
		for i := range v.Len() {
			if c, ok := v.Index(i).Interface().(tea.Cmd); ok {
				out = append(out, menuSent(c)...)
			}
		}
		return out
	}
	return []tea.Msg{msg}
}

// what the menu shows, label then key
func menuLines(m *menu) []string {
	var out []string
	for _, it := range m.items {
		out = append(out, strings.TrimSpace(capital(it.Label)+" "+it.Key))
	}
	return out
}

// a window with buttons and keys, recording what was clicked
type fakeWindow struct {
	acts    []core.Action
	btns    []ui.Btn
	clicked []string
}

func (f *fakeWindow) Update(msg tea.Msg, _ *core.State) (core.Screen, tea.Cmd) {
	if c, ok := msg.(core.Click); ok {
		f.clicked = append(f.clicked, c.ID)
		return f, core.Close(f)
	}
	return f, nil
}
func (f *fakeWindow) View(*core.State, int, int) *lipgloss.Layer { return lipgloss.NewLayer("") }
func (f *fakeWindow) Actions(*core.State) []core.Action          { return f.acts }
func (f *fakeWindow) Animating(*core.State) bool                 { return false }
func (f *fakeWindow) Buttons(*core.State) []ui.Btn               { return f.btns }

func TestMenuListsAConfirmsButtons(t *testing.T) {
	st := &core.State{}
	ran := false
	c := &confirm.Confirm{Question: "Remove it?", Do: "Remove", Run: func(*core.State) tea.Cmd {
		ran = true
		return nil
	}}
	confirm.Open(c)
	m := newMenu(c, st, nil)
	require.Equal(t, []string{"Cancel esc", "Remove"}, menuLines(m), "the real buttons, not Select and Cancel")

	m.Update(core.Press("down"), st)
	_, cmd := m.Update(core.Press("enter"), st)
	require.True(t, ran, "Remove was pressed")
	require.Equal(t, []tea.Msg{core.CloseWindow{Screen: m}, core.CloseWindow{Screen: c}}, menuSent(cmd))
}

func TestMenuSkipsKeysThatOnlyMoveFocus(t *testing.T) {
	st := &core.State{}
	f := &fakeWindow{
		acts: []core.Action{{Key: "enter", Label: "change"}, {Key: "tab", Label: "move"}, {Key: "← →", Label: "choose"},
			{Key: "c", Label: "clear"}, {Key: "ctrl+r", Label: "show"}, {Key: "esc", Label: "close"}},
		btns: []ui.Btn{{ID: "close", Label: "Close"}, {ID: "clear", Label: "Clear", Danger: true}},
	}
	m := newMenu(f, st, []core.Action{{Key: "q", Label: "quit"}})
	require.Equal(t, []string{"Close esc", "Clear c", "Change enter", "Show ctrl+r", "", "Quit q"}, menuLines(m))
	require.True(t, m.items[1].Danger)

	_, cmd := m.Update(core.Press("c"), st)
	require.Equal(t, []string{"btn:clear"}, f.clicked, "a letter presses its button")
	require.Equal(t, []tea.Msg{core.CloseWindow{Screen: m}, core.CloseWindow{Screen: f}}, menuSent(cmd))

	_, cmd = m.Update(core.Press("ctrl+r"), st)
	require.Equal(t, []tea.Msg{core.CloseWindow{Screen: m}, menuRun{f, "ctrl+r"}}, menuSent(cmd), "a key still goes to the window")
}

func TestMenuWithoutButtonsKeepsItsKeys(t *testing.T) {
	st := &core.State{}
	f := &fakeWindow{acts: []core.Action{{Key: "enter", Label: "open"}, {Key: "↑↓", Label: "move"}, {Key: "r", Label: "refresh"}}}
	m := newMenu(f, st, nil)
	require.Equal(t, []string{"Open enter", "Refresh r"}, menuLines(m))
	require.Equal(t, []string{"↑↓", "enter", "esc"}, actionKeys(m.Actions(st)))

	one := newMenu(&fakeWindow{acts: []core.Action{{Key: "enter", Label: "open"}}}, st, nil)
	require.Equal(t, []string{"enter", "esc"}, actionKeys(one.Actions(st)), "no ↑↓ with one item")
}

func actionKeys(acts []core.Action) []string {
	var out []string
	for _, a := range acts {
		out = append(out, a.Key)
	}
	return out
}

func TestNoticeScrollsLongText(t *testing.T) {
	st := &core.State{}
	words := strings.Repeat("the quick brown fox jumps over the lazy dog ", 40)
	m := &info{title: "dokky", text: words}
	m.View(st, 60, 14)
	require.Greater(t, m.total, m.page, "the text is taller than the window")
	require.Equal(t, []string{"enter", "↑↓", "esc"}, actionKeys(m.Actions(st)))

	m.Update(core.Press("down"), st)
	require.Equal(t, 1, m.top)
	m.Update(core.Press("up"), st)
	m.Update(core.Press("up"), st)
	require.Equal(t, 0, m.top, "it stops at the top")
	m.Update(core.Press("pgdown"), st)
	require.Equal(t, max(1, m.page-1), m.top)
	m.Update(core.Press("end"), st)
	require.Equal(t, m.total-m.page, m.top)
	m.Update(core.Press("down"), st)
	require.Equal(t, m.total-m.page, m.top, "and at the end")
	m.Update(core.Press("home"), st)
	require.Equal(t, 0, m.top)

	_, cmd := m.Update(core.Press("enter"), st)
	require.Equal(t, core.CloseWindow{Screen: m}, cmd())
}

func TestNoticeShortTextHasNoScroll(t *testing.T) {
	st := &core.State{}
	m := &info{title: "dokky", text: "Saved."}
	m.View(st, 60, 30)
	require.Equal(t, []string{"enter", "esc"}, actionKeys(m.Actions(st)))
	require.Equal(t, []string{"OK enter", "Close esc"}, menuLines(newMenu(m, st, nil)))
}
