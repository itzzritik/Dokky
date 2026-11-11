package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeBand struct {
	w, rows, steps int
	calm           []image.Rectangle
}

func (b *fakeBand) Size() (int, int)         { return b.w, b.rows }
func (b *fakeBand) Step()                    { b.steps++ }
func (b *fakeBand) Calm(r []image.Rectangle) { b.calm = r }
func (b *fakeBand) Lines() []string {
	out := make([]string, b.rows)
	for i := range out {
		out[i] = strings.Repeat("~", b.w)
	}
	return out
}

func TestStageFillsTheScreenBehindTheCard(t *testing.T) {
	var made *fakeBand
	s := &Stage{New: func(w, rows int) Band { made = &fakeBand{w: w, rows: rows}; return made }}
	card := func(int) *Window { w := &Window{W: 52, Title: "Unlock dokky"}; w.Fit(3, 99); return w }
	out, _, _ := Compose(100, 30, Pass{Layer: s.Layer(100, 30, time.Now(), card)})
	lines := strings.Split(out, "\n")
	require.NotNil(t, made)
	require.Equal(t, [2]int{100, 30}, [2]int{made.w, made.rows}, "the band fills the whole screen")
	require.Contains(t, lines[0], "~")
	require.Contains(t, lines[29], "~")
	require.NotContains(t, lines[14], "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~", "the card covers its middle")
	require.Len(t, made.calm, 2, "the card and the wordmark are kept quiet")
	require.True(t, made.calm[0].Overlaps(image.Rect(40, 14, 41, 15)))

	require.Nil(t, s.Behind(30, 10, time.Now()), "a screen too small has no band")
	require.False(t, s.Moving())
}
