package core

import (
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Action struct {
	Key, Label     string
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
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "space": tea.KeySpace,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "backspace": tea.KeyBackspace,
}

func Press(k string) tea.KeyPressMsg {
	if code, ok := named[k]; ok {
		return tea.KeyPressMsg{Code: code}
	}
	if utf8.RuneCountInString(k) != 1 {
		return tea.KeyPressMsg{}
	}
	r, _ := utf8.DecodeRuneInString(k)
	return tea.KeyPressMsg{Code: r, Text: k}
}
