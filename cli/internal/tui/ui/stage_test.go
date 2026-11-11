package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeBand struct {
	w, rows, steps int
	drops          []Particle
}

func (b *fakeBand) Size() (int, int) { return b.w, b.rows }
func (b *fakeBand) Step()            { b.steps++ }
func (b *fakeBand) Lines() []string {
	out := make([]string, b.rows)
	for i := range out {
		out[i] = strings.Repeat("~", b.w)
	}
	return out
}
func (b *fakeBand) Particles(int) []Particle { return b.drops }

func TestStageLaysTheBandUnderTheCard(t *testing.T) {
	var made *fakeBand
	s := &Stage{New: func(w, rows int) Band {
		made = &fakeBand{w: w, rows: rows, drops: []Particle{{X: 50, Row: 12, Glyph: "*"}, {X: 2, Row: 2, Glyph: "*"}}}
		return made
	}}
	card := func(int) *Window { w := &Window{W: 52, Title: "Unlock dokky"}; w.Fit(3, 99); return w }
	out, _, _ := Compose(100, 30, Pass{Layer: s.Layer(100, 30, time.Now(), card)})
	lines := strings.Split(out, "\n")
	require.NotNil(t, made)
	require.Equal(t, 10, made.rows, "the band fills the bottom third")
	require.Contains(t, lines[29], "~")
	require.NotContains(t, lines[19], "~")
	require.Contains(t, lines[2], "*", "spray shows in the open")
	require.NotContains(t, lines[12], "*", "but never over the card")

	tall := func(maxH int) *Window { w := &Window{W: 52}; w.Fit(40, maxH); return w }
	s.Layer(100, 30, time.Now(), tall)
	require.False(t, s.Moving(), "a card that needs the room drops the band")
}
