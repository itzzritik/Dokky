package app

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

func (a *App) click(m tea.Mouse) tea.Cmd {
	if m.Button != tea.MouseLeft || a.hits == nil {
		return nil
	}
	id := a.hits.At(m.X, m.Y)
	switch {
	case id == "":
		return nil
	case id == "toast":
		a.toast = nil
		return nil
	case strings.HasPrefix(id, "key:"):
		if k := core.Press(strings.TrimPrefix(id, "key:")); k.Code != 0 {
			return a.key(k)
		}
		return nil
	case strings.HasPrefix(id, "tab:"):
		i, _ := strconv.Atoi(strings.TrimPrefix(id, "tab:"))
		return a.setTab(i)
	}
	return a.updateTop(core.Click{ID: id})
}
