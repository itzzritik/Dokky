package app

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/health"
)

type fixed struct {
	n, failed int
	err       error
	reopen    bool
	auto      bool
}

// every repair in turn, off the update loop; the pass on open says nothing when nothing needed it
func (a *App) fixAll(auto bool) tea.Cmd {
	todo := health.Repairs(a.st, auto)
	if a.st.Fixing || len(todo) == 0 {
		return nil
	}
	a.st.Fixing = true
	return func() tea.Msg {
		var errs []error
		reopen := false
		for _, c := range todo {
			if err := c.Repair(); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", c.Name, err))
				continue
			}
			reopen = reopen || c.Reopen
		}
		return fixed{n: len(todo), failed: len(errs), err: errors.Join(errs...), reopen: reopen, auto: auto}
	}
}

func (a *App) onFixed(m fixed) tea.Cmd {
	a.st.Fixing = false
	switch {
	case m.reopen && m.err == nil:
		return core.Send(core.Reopen{})
	// the pass on open stays a short note; Health has the details
	case m.err != nil && m.auto:
		return tea.Batch(core.Send(core.Toast{Text: "Couldn't fix " + core.Count(m.failed, "problem") + ". See Health.", Bad: true}), a.runChecks())
	case m.err != nil:
		return tea.Batch(core.Fail(m.err), a.runChecks())
	}
	return tea.Batch(core.Say("Fixed "+core.Count(m.n, "problem")), a.runChecks())
}
