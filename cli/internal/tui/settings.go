package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/store"
)

type settings struct {
	cursor int
	cfg    store.Settings
	edit   *textinput.Model
	editID string
}

type settingsMsg store.Settings

func (a *app) fetchSettings() tea.Cmd {
	return fetch(a, "settings.get", nil, func(s store.Settings) tea.Msg { return settingsMsg(s) })
}

func (a *app) setKey(k tea.KeyPressMsg) tea.Cmd {
	s := &a.set
	mounts := a.status.Mounts
	if s.edit != nil {
		switch k.String() {
		case "esc":
			s.edit = nil
		case "enter":
			p := map[string]string{"id": s.editID, "mountPoint": strings.TrimSpace(s.edit.Value())}
			s.edit = nil
			return a.do("mount.setLocal", p, a.refreshStatus())
		default:
			var cmd tea.Cmd
			*s.edit, cmd = s.edit.Update(k)
			return cmd
		}
		return nil
	}
	n := len(mounts) + 1
	switch k.String() {
	case "up", "k":
		s.cursor = clamp(s.cursor-1, n)
	case "down", "j":
		s.cursor = clamp(s.cursor+1, n)
	case "enter", "space":
		c := clamp(s.cursor, n)
		if c == len(mounts) {
			s.cfg.AutoUpdate = !s.cfg.AutoUpdate
			return a.do("settings.set", s.cfg, a.fetchSettings())
		}
		ti := textinput.New()
		ti.Placeholder = "empty for the default"
		if p := mounts[c].MountPoint; p != "*" {
			ti.SetValue(p)
		}
		s.edit, s.editID = &ti, mounts[c].ID
		return ti.Focus()
	}
	return nil
}

func (a *app) setMsg(msg tea.Msg) tea.Cmd {
	s := &a.set
	switch msg := msg.(type) {
	case settingsMsg:
		s.cfg = store.Settings(msg)
	default:
		if s.edit != nil {
			var cmd tea.Cmd
			*s.edit, cmd = s.edit.Update(msg)
			return cmd
		}
	}
	return nil
}

func (a *app) setView() (string, string) {
	s := &a.set
	mounts := a.status.Mounts
	var b strings.Builder
	if s.edit != nil {
		for _, m := range mounts {
			if m.ID == s.editID {
				fmt.Fprintf(&b, "Mount point for %s on this device:\n%s\n", m.Name, s.edit.View())
			}
		}
		return b.String(), "enter save · esc cancel"
	}
	b.WriteString(bold.Render("Mount points on this device") + "\n")
	cur := clamp(s.cursor, len(mounts)+1)
	w := 4
	for _, m := range mounts {
		w = max(w, len(m.Name))
	}
	for i, m := range mounts {
		point := m.MountPoint
		if point == "*" {
			point = "next free drive letter"
		}
		fmt.Fprintf(&b, "%s%-*s  %s\n", cursor(i == cur), w, m.Name, point)
	}
	auto := "off"
	if s.cfg.AutoUpdate {
		auto = "on"
	}
	fmt.Fprintf(&b, "\n%sAuto update: %s\n", cursor(cur == len(mounts)), auto)
	account := "not logged in"
	if a.status.LoggedIn {
		account = "logged in"
	}
	fmt.Fprintf(&b, "\nAccount: %s\nVersion: dokky %s, daemon %s\n", account, buildinfo.Version, a.status.Version)
	return b.String(), "enter change"
}
