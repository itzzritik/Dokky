package ui

import (
	"os"
	"strings"
)

type Glyphs struct {
	Check, Dash, Bang, Cross, Bar, Arrow, Back, Caret, Up, Down, Heavy, H, V string
	TL, TR, BL, BR, Top, Bottom, Mask, Ellipsis, Toast                       string
	Spinner                                                                  []string
	ASCII                                                                    bool
}

var G = pick()

func pick() Glyphs {
	if v := strings.ToLower(os.Getenv("DOKKY_ASCII")); v == "1" || v == "true" || !terminalSupportsUnicode() {
		return Glyphs{Check: "+", Dash: "-", Bang: "!", Cross: "x", Bar: "|", Arrow: ">", Back: "<", Caret: "v", Up: "^", Down: "v", Heavy: "=", H: "-", V: "|",
			TL: "+", TR: "+", BL: "+", BR: "+", Top: " ", Bottom: " ", Mask: "*", Ellipsis: "~", Toast: "|", Spinner: []string{"|", "/", "-", "\\"}, ASCII: true}
	}
	return Glyphs{Check: "✓", Dash: "–", Bang: "!", Cross: "✕", Bar: "▎", Arrow: "›", Back: "‹", Caret: "▾", Up: "↑", Down: "↓", Heavy: "━", H: "─", V: "│",
		TL: "╭", TR: "╮", BL: "╰", BR: "╯", Top: "▄", Bottom: "▀", Mask: "•", Ellipsis: "…", Toast: "▌",
		Spinner: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}}
}
