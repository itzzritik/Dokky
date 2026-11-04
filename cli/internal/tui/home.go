package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/itzzritik/dokky/cli/internal/ipc"
)

type home struct {
	cursor   int
	pending  map[string]string
	add      *addMount
	deleting *daemon.MountStatus
	unsent   string
}

type unsentMsg struct {
	m   daemon.MountStatus
	msg string
}

type mountDoneMsg struct {
	name string
	err  error
}

func (a *app) homeKey(k tea.KeyPressMsg) tea.Cmd {
	if a.home.add != nil {
		return a.addKey(k)
	}
	if m := a.home.deleting; m != nil {
		switch k.String() {
		case "y":
			force := a.home.unsent != ""
			a.home.deleting, a.home.unsent = nil, ""
			return a.deleteMount(*m, force)
		case "n", "esc":
			a.home.deleting, a.home.unsent = nil, ""
		}
		return nil
	}
	mounts := a.status.Mounts
	switch k.String() {
	case "up", "k":
		a.home.cursor = clamp(a.home.cursor-1, len(mounts))
	case "down", "j":
		a.home.cursor = clamp(a.home.cursor+1, len(mounts))
	case "a":
		a.home.add = &addMount{}
		return a.fetchServices()
	case "d":
		if len(mounts) > 0 {
			m := mounts[clamp(a.home.cursor, len(mounts))]
			a.home.deleting = &m
		}
	case "enter":
		if len(mounts) == 0 {
			return nil
		}
		m := mounts[clamp(a.home.cursor, len(mounts))]
		if _, busy := a.home.pending[m.Name]; busy || m.State == "mounting" {
			return nil
		}
		return a.toggleMount(m.Name, m.State == "unmounted" || m.State == "error")
	}
	return nil
}

func (a *app) toggleMount(name string, mount bool) tea.Cmd {
	method, word := "unmount", "unmounting"
	if mount {
		method, word = "mount", "mounting"
	}
	a.home.pending[name] = word
	return func() tea.Msg {
		ctx, cancel := a.callCtx()
		defer cancel()
		return mountDoneMsg{name: name, err: a.d.Call(ctx, method, map[string]string{"name": name}, nil)}
	}
}

func (a *app) deleteMount(m daemon.MountStatus, force bool) tea.Cmd {
	params := map[string]any{"id": m.ID}
	if force {
		params["force"] = true
	}
	return func() tea.Msg {
		ctx, cancel := a.callCtx()
		defer cancel()
		err := a.d.Call(ctx, "mount.delete", params, nil)
		var ie *ipc.Error
		if errors.As(err, &ie) && ie.Code == "unsent" {
			return unsentMsg{m, ie.Message}
		}
		if err != nil {
			return errMsg{err}
		}
		return a.refreshStatus()()
	}
}

func (a *app) homeMsg(msg tea.Msg) tea.Cmd {
	if u, ok := msg.(unsentMsg); ok {
		a.home.deleting, a.home.unsent = &u.m, u.msg
		return nil
	}
	if m, ok := msg.(mountDoneMsg); ok {
		delete(a.home.pending, m.name)
		if m.err != nil {
			a.notice = m.err.Error()
		}
		return a.refreshStatus()
	}
	if a.home.add != nil {
		return a.addMsg(msg)
	}
	return nil
}

func (a *app) homeView() (string, string) {
	if a.home.add != nil {
		return a.addView()
	}
	var b strings.Builder
	if m := a.home.deleting; m != nil && a.home.unsent != "" {
		fmt.Fprintf(&b, "Delete %s anyway?\n\n%s\n", m.Name, a.home.unsent)
		return b.String(), "y delete and drop them · n keep"
	}
	if m := a.home.deleting; m != nil {
		fmt.Fprintf(&b, "Delete %s?\n\n", m.Name)
		if m.State == "mounted" {
			b.WriteString("It is mounted, so it is unmounted first.\n")
		}
		if n := m.UploadsQueued + m.UploadsInProgress; n > 0 {
			fmt.Fprintf(&b, "%d uploads have not finished; deleting now stops them.\n", n)
		}
		b.WriteString("Your files on the service are not touched.\n")
		return b.String(), "y delete · n keep"
	}
	mounts := a.status.Mounts
	if len(mounts) == 0 {
		b.WriteString("No mounts yet. Press a to add one.\n")
	} else {
		w := 4
		for _, m := range mounts {
			w = max(w, len(m.Name))
		}
		fmt.Fprintf(&b, "  %-*s  %-12s  %-10s  %s\n", w, "NAME", "STATUS", "CACHE", "UPLOADS")
		cur := clamp(a.home.cursor, len(mounts))
		for i, m := range mounts {
			state := m.State
			if p, ok := a.home.pending[m.Name]; ok {
				state = p
			}
			word := stateWord(state)
			if m.RemountToApply {
				word += " " + yellow.Render("(remount to apply)")
			}
			fmt.Fprintf(&b, "%s%-*s  %s  %-10s  %d queued, %d uploading\n", cursor(i == cur), w, m.Name,
				pad(word, 12), size(m.CacheBytes), m.UploadsQueued, m.UploadsInProgress)
			if m.State == "error" && m.Error != "" {
				fmt.Fprintf(&b, "  %s  %s\n", strings.Repeat(" ", w), red.Render(m.Error))
				if a.driverFix != "" {
					fmt.Fprintf(&b, "  %s  fix: %s\n", strings.Repeat(" ", w), a.driverFix)
				}
			}
		}
	}
	var queued, active int
	for _, m := range mounts {
		queued += m.UploadsQueued
		active += m.UploadsInProgress
	}
	fmt.Fprintf(&b, "\nUploads: %d queued, %d uploading    Free space: %s\n", queued, active, size(int64(a.status.FreeBytes)))
	return b.String(), "enter mount/unmount · a add mount · d delete"
}

func pad(styled string, to int) string {
	return styled + strings.Repeat(" ", max(0, to-lipgloss.Width(styled)))
}
