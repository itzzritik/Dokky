package app

import (
	"context"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

const restartIn = 3 * time.Second

type latest struct{ version string }

type found struct {
	version string
	err     error
}

type installed struct {
	version string
	err     error
}

type restartNow struct{}

// once a session, never before the TUI shows: with automatic updates on, npm installs the newest in the background and dokky restarts onto it; off, it is offered
func (a *App) checkUpdate() tea.Cmd {
	if a.updateChecked || !a.st.Connected || a.deps.Reopened || strings.Contains(buildinfo.Version, "dev") {
		return nil
	}
	a.updateChecked = true
	c := a.st.C
	if !a.st.Snap.Settings.AutoUpdate {
		return func() tea.Msg {
			v, _ := c.UpdateCheck(context.Background())
			return latest{v}
		}
	}
	a.st.Updating = "Checking for updates"
	return func() tea.Msg {
		v, err := c.UpdateCheck(context.Background())
		return found{v, err}
	}
}

func (a *App) onFound(m found) tea.Cmd {
	a.st.Updating = ""
	if m.err != nil || m.version == "" {
		return nil
	}
	a.st.Latest = m.version
	return a.startUpdate()
}

// installs Latest with npm in the background; the header shows the step and a failure is a toast
func (a *App) startUpdate() tea.Cmd {
	if !a.st.UpdateReady() || a.st.C == nil {
		return nil
	}
	a.st.Updating = "Updating to " + a.st.Latest
	c := a.st.C
	return func() tea.Msg {
		v, err := c.InstallUpdate(context.Background())
		return installed{v, err}
	}
}

// a failed install keeps Latest, so u, Settings and the footer still offer it
func (a *App) onInstalled(m installed) tea.Cmd {
	a.st.Updating = ""
	if m.err != nil {
		return core.Send(core.Toast{Text: core.Fit("Couldn't update: "+core.Report(m.err), longNotice), Bad: true, Now: true})
	}
	a.st.Latest = ""
	if m.version == "" || m.version == buildinfo.Version {
		return core.Say("dokky is up to date")
	}
	a.pendingRestart = m.version
	return a.restartWhenIdle()
}

// a restart drops the mounts for a moment, so it waits for uploads to finish
func (a *App) restartWhenIdle() tea.Cmd {
	if a.pendingRestart == "" || a.restartOpen {
		return nil
	}
	uploads := 0
	for _, d := range a.st.Snap.Drives {
		uploads += d.Uploads
	}
	if uploads > 0 {
		a.st.Updating = "Update ready"
		return nil
	}
	a.restartOpen, a.restartAt, a.st.Updating = true, a.st.Now, ""
	return tea.Tick(restartIn, func(time.Time) tea.Msg { return restartNow{} })
}

// the service restarts onto the new dokky unless it already runs it, then this TUI opens again on it and carries Updating on
func (a *App) onRestartNow() tea.Cmd {
	c, running, next := a.st.C, a.st.Snap.Version, a.pendingRestart
	_ = os.Setenv(updatingEnv, buildinfo.Version+" "+next)
	return func() tea.Msg {
		if c != nil && running != next {
			_ = c.RestartDaemon(context.Background())
		}
		return core.Reopen{}
	}
}

// once a session, with automatic updates off
func (a *App) onLatest(m latest) tea.Cmd {
	a.st.Latest = m.version
	if !a.st.UpdateReady() {
		return nil
	}
	return core.Say("dokky " + m.version + " is out. Press u to update.")
}
