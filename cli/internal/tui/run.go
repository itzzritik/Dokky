package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/app"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

type (
	Deps  = core.Deps
	Check = core.Check
)

const (
	Good = core.Good
	Warn = core.Warn
	Bad  = core.Bad
)

// the TUI quit so dokky opens again, after an update or a repair
var ErrReopen = errors.New("reopen dokky")

func Run(ctx context.Context, deps Deps) error {
	m, err := tea.NewProgram(app.New(deps), tea.WithContext(ctx)).Run()
	if a, ok := m.(*app.App); ok && err == nil && a.Reopening() {
		return ErrReopen
	}
	return err
}
