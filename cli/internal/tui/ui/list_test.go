package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/stretchr/testify/require"
)

func TestMove(t *testing.T) {
	for name, c := range map[string]struct {
		k            string
		sel, n, page int
		want         int
		ok           bool
	}{
		"down":               {"down", 2, 10, 4, 3, true},
		"down at the end":    {"down", 9, 10, 4, 9, true},
		"up at the top":      {"up", 0, 10, 4, 0, true},
		"home":               {"home", 7, 10, 4, 0, true},
		"end":                {"end", 2, 10, 4, 9, true},
		"page down":          {"pgdown", 2, 10, 4, 6, true},
		"page down stops":    {"pgdown", 8, 10, 4, 9, true},
		"page up stops":      {"pgup", 2, 10, 4, 0, true},
		"no page is one row": {"pgdown", 2, 10, 0, 3, true},
		"empty list":         {"end", 0, 0, 4, 0, true},
		"not a list key":     {"enter", 3, 10, 4, 3, false},
	} {
		got, ok := Move(c.k, c.sel, c.n, c.page)
		require.Equal(t, c.ok, ok, name)
		require.Equal(t, c.want, got, name)
	}
}

// every cell's click ID along row y
func hitsOn(h Hits, w, y int) map[string]bool {
	ids := map[string]bool{}
	for x := 0; x < w; x++ {
		if id := h.At(x, y); id != "" {
			ids[id] = true
		}
	}
	return ids
}

func TestBannerKeyCanBeClicked(t *testing.T) {
	b := NewBox()
	PutBanner(b, 0, 80, 3, false, "Signed out on the server", "s", "remove this device")
	_, hits, _ := Compose(80, 3, Pass{Layer: b.Layer()})
	require.True(t, hitsOn(hits, 80, 1)["key:s"])
	require.Empty(t, hitsOn(hits, 80, 0), "only the key's line")
}

func TestFooterHints(t *testing.T) {
	more := core.Action{Key: "m", Label: "More"}
	left := []core.Action{{Key: "↑↓", Label: "move"}, {Key: "enter", Label: "open"}}
	out, hits, _ := Compose(100, 1, Pass{Layer: Footer(100, left, []core.Action{more, {Key: "q", Label: "Quit"}}, "")})
	ids := hitsOn(hits, 100, 0)
	require.False(t, ids["key:↑↓"], "up and down are no one key to press")
	require.True(t, ids["key:enter"])
	require.True(t, ids["key:q"])
	require.Contains(t, out, "Open", "labels start with a capital")

	// short on room, the right side keeps only More, then the last screen keys go
	many := append(left, core.Action{Key: "space", Label: "unmount"}, core.Action{Key: "v", Label: "view in Files"})
	_, hits, _ = Compose(60, 1, Pass{Layer: Footer(60, many, []core.Action{more, {Key: "q", Label: "Quit"}}, "")})
	ids = hitsOn(hits, 60, 0)
	require.True(t, ids["key:m"])
	require.False(t, ids["key:q"])
	require.False(t, ids["key:v"])
}

func TestFooterTailSitsInTheCorner(t *testing.T) {
	left := []core.Action{{Key: "a", Label: "Add"}, {Key: "e", Label: "Edit"}}
	right := []core.Action{{Key: "m", Label: "More"}, {Key: "q", Label: "Quit"}}
	out, hits, _ := Compose(100, 1, Pass{Layer: Footer(100, left, right, "v0.1.40")})
	line := strings.TrimRight(ansi.Strip(out), " ")
	require.True(t, strings.HasSuffix(line, "Quit   v0.1.40"), "the version sits right of Quit, in the corner: %q", line)
	require.True(t, hitsOn(hits, 100, 0)["key:q"])

	// short on room, the version goes before any key does
	w := HintsWidth(left, true) + HintsWidth(right, true) + 3 + 6
	out, hits, _ = Compose(w, 1, Pass{Layer: Footer(w, left, right, "v0.1.40")})
	require.NotContains(t, ansi.Strip(out), "v0.1.40")
	ids := hitsOn(hits, w, 0)
	require.True(t, ids["key:e"] && ids["key:q"], "every key and label stays")
	require.Contains(t, ansi.Strip(out), "Quit")
}

func TestWindowHintsKeepEsc(t *testing.T) {
	acts := []core.Action{{Key: "↑↓", Label: "Move"}, {Key: "space", Label: "Show password"}, {Key: "esc", Label: "Quit"}}
	win := &Window{W: 52, H: 6, Title: "Enter your master password", Buttons: []Btn{{ID: "a", Label: "Recovery key"}, {ID: "go", Label: "Continue", Primary: true}}, Hints: acts}
	out, hits, _ := Compose(52, 6, Pass{Layer: win.Layer()})
	require.Contains(t, out, "Quit", "short on room, esc stays")
	require.NotContains(t, out, "Show password", "the keys before it go")
	ids := hitsOn(hits, 52, 4)
	require.True(t, ids["key:esc"], "the hint clicks as its key")
}
