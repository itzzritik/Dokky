package ui

import (
	"slices"
	"strings"
)

// Keyboard focus on a screen is the click ID of what has it, so a click and a key land on the same thing.
// Stops are listed in reading order: fields, links, toggles and tiles first, then the buttons along the bar ("btn:" IDs).

func IsBtn(id string) bool { return strings.HasPrefix(id, "btn:") }

// up and down walk every stop in order, wrapping; left and right walk the buttons along the bar. tab is never
// used: in a browser terminal it moves focus out of the terminal
// Reports whether k moved focus, so the caller can hand other keys to the focused field.
func Walk(stops []string, at *string, k string) bool {
	n := len(stops)
	if n == 0 {
		return false
	}
	i := slices.Index(stops, *at)
	switch k {
	case "down":
		*at = stops[(i+1)%n]
	case "up":
		*at = stops[(max(i, 0)+n-1)%n]
	case "left", "right":
		if i < 0 || !IsBtn(*at) {
			return false
		}
		j := i + 1
		if k == "left" {
			j = i - 1
		}
		if j >= 0 && j < n && IsBtn(stops[j]) {
			*at = stops[j]
		}
	default:
		return false
	}
	return true
}

// enter or space on a focused button presses it
func Pressed(k, at string) bool { return IsBtn(at) && (k == "enter" || k == "space") }
