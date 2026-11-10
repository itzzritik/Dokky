package core

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Action struct {
	Key, Label     string
	Icon           string // shown in the menu; empty draws nothing
	Danger, Accent bool
}

type Screen interface {
	Update(tea.Msg, *State) (Screen, tea.Cmd)
	View(st *State, w, h int) *lipgloss.Layer
	Actions(*State) []Action
	Animating(*State) bool
}

// a screen that is typing into a field gets every key, tabs and quit included
type Capturer interface{ Capturing() bool }

// a window placed in the body (bw by bh) instead of centred, with nothing dimmed under it
type Placer interface {
	Place(bw, bh, w, h int) (x, y int)
}

var named = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "space": tea.KeySpace, "tab": tea.KeyTab,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "backspace": tea.KeyBackspace,
	"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "delete": tea.KeyDelete,
}

func Press(k string) tea.KeyPressMsg {
	if code, ok := named[k]; ok {
		return tea.KeyPressMsg{Code: code}
	}
	for prefix, mod := range map[string]tea.KeyMod{"ctrl+": tea.ModCtrl, "shift+": tea.ModShift, "alt+": tea.ModAlt} {
		if rest, ok := strings.CutPrefix(k, prefix); ok {
			m := Press(rest)
			if m.Code == 0 {
				return tea.KeyPressMsg{}
			}
			m.Mod |= mod
			if mod != tea.ModShift {
				m.Text = ""
			}
			return m
		}
	}
	if utf8.RuneCountInString(k) != 1 {
		return tea.KeyPressMsg{}
	}
	r, _ := utf8.DecodeRuneInString(k)
	return tea.KeyPressMsg{Code: r, Text: k}
}
