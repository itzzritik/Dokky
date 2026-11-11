package topography

import (
	"image"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func braille(s string) int {
	n := 0
	for _, r := range s {
		if r > 0x2800 && r <= 0x28FF {
			n++
		}
	}
	return n
}

func TestLinesAreExactlyWide(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}, {1, 1}, {203, 61}} {
		m := NewAt(size[0], size[1], 40)
		m.age = 10
		lines := Render(m)
		require.Len(t, lines, size[1])
		for _, l := range lines {
			require.Equal(t, size[0], ansi.StringWidth(l))
		}
	}
}

func TestNilIsSafe(t *testing.T) {
	var m *Map
	m.Step()
	m.SetStill(true)
	m.Calm(nil)
	m.SetPalette(Palette{})
	w, h := m.Size()
	require.Zero(t, w+h)
	require.Nil(t, Render(m))
	require.Nil(t, New(0, 3))
}

func TestFillsTheScreenWithLines(t *testing.T) {
	m := NewAt(100, 30, 12)
	m.age = 10
	lines := Render(m)
	require.Greater(t, braille(strings.Join(lines, "")), 600, "contours across the screen")
	require.Positive(t, braille(lines[2]), "the far land reaches the top rows")
	require.Positive(t, braille(lines[28]), "and the near land the bottom")
}

func TestRisesOnFirstShow(t *testing.T) {
	m := NewAt(100, 30, 12)
	require.Zero(t, braille(strings.Join(Render(m), "")), "the map fades in from nothing")
	for range 60 {
		m.Step()
	}
	require.Positive(t, braille(strings.Join(Render(m), "")))
}

func TestCalmKeepsTextClear(t *testing.T) {
	m := NewAt(100, 30, 12)
	m.age = 10
	m.Calm([]image.Rectangle{image.Rect(24, 8, 76, 18)})
	Render(m)
	inside, outside := 0.0, 0.0
	for y := 9 * 4; y < 17*4; y++ {
		for x := 30 * 2; x < 70*2; x++ {
			inside = max(inside, m.lit[y*200+x])
		}
	}
	for y := 22 * 4; y < 30*4; y++ {
		for x := 0; x < 200; x++ {
			outside = max(outside, m.lit[y*200+x])
		}
	}
	require.Less(t, inside, outside*.25, "lines beside the card are a whisper of the rest")
}

func TestStillHoldsTheFrame(t *testing.T) {
	m := NewAt(80, 24, 5)
	m.SetStill(true)
	a := strings.Join(Render(m), "\n")
	m.Step()
	require.Equal(t, a, strings.Join(Render(m), "\n"))
}

func BenchmarkFullScreen(b *testing.B) {
	m := NewAt(100, 30, 0)
	m.age = 10
	for b.Loop() {
		m.Step()
		Render(m)
	}
}

func BenchmarkLargeScreen(b *testing.B) {
	m := NewAt(200, 60, 0)
	m.age = 10
	for b.Loop() {
		m.Step()
		Render(m)
	}
}
