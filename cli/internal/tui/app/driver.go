package app

import (
	"errors"
	"fmt"
	"os/exec"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
)

const winfspPage = "https://winfsp.dev/rel/"

type driverDone struct{ err error }

// WinFsp is missing: install it with winget, which asks Windows for permission
func offerDriver() tea.Cmd {
	return confirm.Open(&confirm.Confirm{
		Question: "Install WinFsp?",
		Line:     "Windows needs it to show your drives as folders. Windows asks for permission first.",
		Do:       "Install",
		Positive: true,
		Run: func(*core.State, bool) tea.Cmd {
			return tea.Batch(core.Say("Installing WinFsp"), func() tea.Msg { return driverDone{platform.InstallWinFsp()} })
		},
	})
}

// the drives that failed for want of WinFsp connect now
func (a *App) onDriverDone(m driverDone) tea.Cmd {
	switch {
	case errors.Is(m.err, exec.ErrNotFound):
		_ = platform.OpenURL(winfspPage)
		return core.Say("Install WinFsp from winfsp.dev, then mount again")
	case m.err != nil:
		return core.Fail(fmt.Errorf("could not install WinFsp: %w", m.err))
	}
	cmds := []tea.Cmd{core.Say("WinFsp is installed")}
	for _, d := range a.st.Snap.Drives {
		if d.State == api.Failed {
			cmds = append(cmds, core.Connect(a.st, d.ID))
		}
	}
	return tea.Batch(cmds...)
}
