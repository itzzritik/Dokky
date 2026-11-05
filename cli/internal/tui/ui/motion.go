package ui

import (
	"image/color"
	"math"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const frame = 33 * time.Millisecond

var reduced = strings.EqualFold(os.Getenv("DOKKY_MOTION"), "reduced")

func Reduced() bool { return reduced }

type TickMsg struct{}

func Tick() tea.Cmd { return tea.Tick(frame, func(time.Time) tea.Msg { return TickMsg{} }) }

// ease-out cubic from start over d
func Ease(from, to float64, start, now time.Time, d time.Duration) float64 {
	if Reduced() || d <= 0 {
		return to
	}
	x := math.Max(0, math.Min(1, float64(now.Sub(start))/float64(d)))
	return from + (to-from)*(1-math.Pow(1-x, 3))
}

func Easing(start, now time.Time, d time.Duration) bool {
	return !Reduced() && now.Sub(start) < d
}

func Spin(t float64) string {
	return G.Spinner[int(t*12)%len(G.Spinner)]
}

// the spinner's colour sweeps along the water ramp
func SpinFg(t float64) color.Color {
	if Reduced() {
		return P().Accent
	}
	return RampAt(.62 + .22*math.Sin(t*1.6))
}

// wave in 0..1 for shimmer at position i
func Wave(i int, k, speed, t float64) float64 {
	if Reduced() {
		return .3
	}
	return .5 + .5*math.Sin(float64(i)*k-t*speed)
}
