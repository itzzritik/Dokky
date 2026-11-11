package vault

import (
	"strings"

	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// the bar's lit button: the focused one, else def, the one enter reaches from where focus is ("" for none)
func lit(win *ui.Window, at, def string) int {
	if i := win.Index(at); i >= 0 {
		return i
	}
	return win.Index(def)
}

func fieldID(i int) string { return "field:" + string(rune('0'+i)) }

// which numbered field has focus, -1 for none
func fieldAt(at string) int {
	if len(at) == 7 && strings.HasPrefix(at, "field:") {
		return int(at[6] - '0')
	}
	return -1
}

// the primary button's words, or a spinner and what is happening
func busyLabel(st *core.State, busy bool, idle, doing string) string {
	if busy {
		return ui.Spin(st.T) + " " + doing
	}
	return idle
}

var moveAction = core.Action{Key: "↑↓", Label: "move"}

// a short error in the sub line, else the usual words
func sub(win *ui.Window, usual, err string) {
	win.Sub, win.SubBad = usual, false
	if err != "" {
		win.Sub, win.SubBad = err, true
	}
}
