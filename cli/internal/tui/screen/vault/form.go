package vault

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// tab and the arrows move between n items; reports whether k was one of them
func move(k string, focus *int, n int) bool {
	switch k {
	case "tab", "down":
		*focus = (*focus + 1) % n
	case "shift+tab", "up":
		*focus = (*focus + n - 1) % n
	default:
		return false
	}
	return true
}

// a line of text that acts, like "Forgot password?", with the list's selection look when focused
func link(id, label string, focused bool, bg color.Color) *lipgloss.Layer {
	p := ui.P()
	return ui.Row(id, ui.Width(label)+4, focused, bg, func(bg color.Color, iw int) string {
		if focused {
			return ui.L(bg).B(label, p.Accent).Pad(iw)
		}
		return ui.L(bg).T(label, p.Muted).Pad(iw)
	})
}

// the primary button's words, or a spinner and what is happening
func busyLabel(st *core.State, busy bool, idle, doing string) string {
	if busy {
		return ui.Spin(st.T) + " " + doing
	}
	return idle
}

// wrapped muted lines for a window's body
func para(win *ui.Window, s string, fg color.Color) []string {
	var out []string
	for _, l := range strings.Split(lipgloss.NewStyle().Width(win.Inner()).Render(s), "\n") {
		out = append(out, win.Text(strings.TrimRight(l, " "), fg))
	}
	return out
}

func revealAction(on bool) core.Action {
	if on {
		return core.Action{Key: "ctrl+r", Label: "hide"}
	}
	return core.Action{Key: "ctrl+r", Label: "show"}
}

// a short error in the sub line, else the usual words
func sub(win *ui.Window, usual, err string) {
	win.Sub, win.SubBad = usual, false
	if err != "" {
		win.Sub, win.SubBad = err, true
	}
}
