package ui

import (
	"os"
	"strings"
)

type Glyphs struct {
	Check, Dash, Bang, Cross, Bar, Arrow, Back, Caret, Up, Down, Heavy, H, V string
	TL, TR, BL, BR, Top, Bottom, Mask, Ellipsis, Toast, Here, Ring, To       string
	Spinner                                                                  []string
	ASCII                                                                    bool
	Icon                                                                     Icons
}

// one glyph per kind of action, for menus, settings rows and card titles; empty in ASCII, so nothing shows
type Icons struct {
	Edit, Mount, Unmount, Files, Add, Find, SignIn, SignOut, Copy, Open, Retry, Fix string
	Lock, Quit, Update, Remove, Details, Devices, Clock, Key, Cache, Sync, Health   string
}

var G = pick()

func pick() Glyphs {
	if v := strings.ToLower(os.Getenv("DOKKY_ASCII")); v == "1" || v == "true" || !terminalSupportsUnicode() {
		return Glyphs{Check: "+", Dash: "-", Bang: "!", Cross: "x", Bar: "|", Arrow: ">", Back: "<", Caret: "v", Up: "^", Down: "v", Heavy: "=", H: "-", V: "|",
			TL: "+", TR: "+", BL: "+", BR: "+", Top: " ", Bottom: " ", Mask: "*", Ellipsis: "~", Toast: "|", Here: "*", Ring: "o", To: "->", Spinner: []string{"|", "/", "-", "\\"}, ASCII: true}
	}
	return Glyphs{Check: "✓", Dash: "–", Bang: "!", Cross: "✕", Bar: "▎", Arrow: "›", Back: "‹", Caret: "▾", Up: "↑", Down: "↓", Heavy: "━", H: "─", V: "│",
		TL: "╭", TR: "╮", BL: "╰", BR: "╯", Top: "▄", Bottom: "▀", Mask: "•", Ellipsis: "…", Toast: "▌", Here: "●", Ring: "○", To: "→",
		Spinner: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		Icon: Icons{Edit: "✎", Mount: "↧", Unmount: "⏏", Files: "⎕", Add: "⊕", Find: "⌕", SignIn: "⇥", SignOut: "⎋", Copy: "❐", Open: "↗",
			Retry: "↺", Fix: "✚", Lock: "⊡", Quit: "⊗", Update: "↥", Remove: "⌫", Details: "⌸", Devices: "⇄", Clock: "◷", Key: "✱",
			Cache: "◍", Sync: "↻", Health: "✚"}}
}
