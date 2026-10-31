package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
)

const lastSeenLayout = "2 Jan 2006 15:04"

type devices struct {
	list     []dsync.Device
	loaded   bool
	cursor   int
	revoking *dsync.Device
}

type devicesMsg []dsync.Device

func (a *app) fetchDevices() tea.Cmd {
	if !a.status.LoggedIn {
		return nil
	}
	return fetch(a, "devices.list", nil, func(l []dsync.Device) tea.Msg { return devicesMsg(l) })
}

func (a *app) devKey(k tea.KeyPressMsg) tea.Cmd {
	v := &a.dev
	if v.revoking != nil {
		switch k.String() {
		case "y":
			id := v.revoking.ID
			v.revoking = nil
			return a.do("devices.revoke", map[string]string{"id": id}, a.fetchDevices())
		case "n", "esc":
			v.revoking = nil
		}
		return nil
	}
	n := len(v.list)
	switch k.String() {
	case "up", "k":
		v.cursor = clamp(v.cursor-1, n)
	case "down", "j":
		v.cursor = clamp(v.cursor+1, n)
	case "r":
		if n == 0 {
			return nil
		}
		dv := v.list[clamp(v.cursor, n)]
		if dv.Current {
			a.notice = "This is this device. Run dokky logout to remove it."
			return nil
		}
		v.revoking = &dv
	}
	return nil
}

func (a *app) devView() (string, string) {
	v := &a.dev
	switch {
	case !a.status.LoggedIn:
		return "Log in to see devices.\n", ""
	case v.revoking != nil:
		return fmt.Sprintf("Revoke %s?\n\nThis device will be wiped, including any uploads it has not sent yet.\n", v.revoking.Name), "y revoke · n keep"
	case !v.loaded:
		return "Loading...\n", ""
	}
	var b strings.Builder
	w := 4
	for _, dv := range v.list {
		w = max(w, len(dv.Name))
	}
	fmt.Fprintf(&b, "  %-*s  %-8s  %-10s  %s\n", w, "NAME", "OS", "VERSION", "LAST SEEN")
	cur := clamp(v.cursor, len(v.list))
	for i, dv := range v.list {
		seen := "never"
		if !dv.LastSeenAt.IsZero() {
			seen = dv.LastSeenAt.Local().Format(lastSeenLayout)
		}
		if dv.Current {
			seen += "  " + bold.Render("this device")
		}
		fmt.Fprintf(&b, "%s%-*s  %-8s  %-10s  %s\n", cursor(i == cur), w, dv.Name, dv.OS, dv.Version, seen)
	}
	return b.String(), "r revoke"
}
