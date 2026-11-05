package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/app"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

type Deps = core.Deps

func Run(ctx context.Context, deps Deps) error {
	_, err := tea.NewProgram(app.New(deps), tea.WithContext(ctx)).Run()
	return err
}
