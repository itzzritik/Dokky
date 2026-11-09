package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPressReadsBackAsTheSameKey(t *testing.T) {
	for _, k := range []string{"enter", "esc", "space", "tab", "shift+tab", "up", "down", "left", "right", "home", "end", "pgup", "pgdown", "ctrl+l", "ctrl+r", "a", "?", "/"} {
		require.Equal(t, k, Press(k).String(), k)
	}
	require.Zero(t, Press("↑↓").Code, "a hint that stands for several keys is not one key")
}
