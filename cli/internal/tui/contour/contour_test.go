package contour

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestLinesAreExactlyWide(t *testing.T) {
	for _, size := range [][2]int{{100, 10}, {40, 4}, {1, 1}, {203, 13}} {
		m := NewAt(size[0], size[1], 42)
		lines := Render(m)
		require.Len(t, lines, size[1])
		for _, l := range lines {
			require.Equal(t, size[0], ansi.StringWidth(l), "every row fills the band")
		}
	}
}

func TestNilIsSafe(t *testing.T) {
	var m *Map
	m.Step()
	m.SetStill(true)
	m.SetPalette(Palette{})
	w, h := m.Size()
	require.Zero(t, w+h)
	require.Nil(t, Render(m))
	require.Nil(t, New(0, 5))
}

func TestSameTimeSameMap(t *testing.T) {
	a, b := NewAt(80, 8, 7), NewAt(80, 8, 7)
	require.Equal(t, Render(a), Render(b))
	b.Step()
	require.NotEqual(t, Render(a), Render(b), "a step moves the terrain")
	b.SetStill(true)
	frozen := strings.Join(Render(b), "\n")
	b.Step()
	require.Equal(t, frozen, strings.Join(Render(b), "\n"), "still holds the frame for reduced motion")
}

func TestDrawsLines(t *testing.T) {
	out := strings.Join(Render(NewAt(100, 10, 3)), "")
	require.True(t, strings.ContainsFunc(out, func(r rune) bool { return r > 0x2800 && r <= 0x28FF }), "the map has contour dots")
}

func BenchmarkFrame(b *testing.B) {
	m := NewAt(200, 14, 0)
	for b.Loop() {
		m.Step()
		Render(m)
	}
}
