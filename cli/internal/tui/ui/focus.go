package ui

import (
	"slices"
	"strings"
)

// Keyboard focus on a screen is the click ID of what has it, so a click and a key land on the same thing.
// Stops are listed in reading order: fields, links, toggles and tiles first, then the buttons along the bar ("btn:" IDs).

func IsBtn(id string) bool { return strings.HasPrefix(id, "btn:") }

// up and down walk every stop in order, wrapping; left and right walk the buttons along the bar. A field's Show
// toggle (EyeID) listed among the stops sits right of its field: right reaches it, left goes back, and up or down
// move on as from the field. tab is never used: in a browser terminal it moves focus out of the terminal.
// Reports whether k moved focus, so the caller can hand other keys to the focused field.
func Walk(stops []string, at *string, k string) bool {
	if field, ok := strings.CutPrefix(*at, "eye:"); ok {
		switch k {
		case "left":
			*at = field
			return true
		case "right":
			return true
		case "up", "down":
			*at = field
		}
	}
	if k == "right" && slices.Contains(stops, EyeID(*at)) {
		*at = EyeID(*at)
		return true
	}
	line := slices.DeleteFunc(slices.Clone(stops), IsEye)
	n := len(line)
	if n == 0 {
		return false
	}
	i := slices.Index(line, *at)
	switch k {
	case "down":
		*at = line[(i+1)%n]
	case "up":
		*at = line[(max(i, 0)+n-1)%n]
	case "left", "right":
		if i < 0 || !IsBtn(*at) {
			return false
		}
		j := i + 1
		if k == "left" {
			j = i - 1
		}
		if j >= 0 && j < n && IsBtn(line[j]) {
			*at = line[j]
		}
	default:
		return false
	}
	return true
}

// enter or space on a focused button, or a field's Show toggle, presses it
func Pressed(k, at string) bool { return (IsBtn(at) || IsEye(at)) && (k == "enter" || k == "space") }
