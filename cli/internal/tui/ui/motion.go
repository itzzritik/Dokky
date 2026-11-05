package ui

import (
	"math"
	"os"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

const frame = 33 * time.Millisecond

var (
	envReduced = strings.EqualFold(os.Getenv("DOKKY_MOTION"), "reduced")
	reduced    atomic.Bool
)

func init() { reduced.Store(envReduced) }

func Reduced() bool { return reduced.Load() }

func SetMotion(m string) { reduced.Store(envReduced || m == "reduced") }

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

// wave in 0..1 for shimmer at position i
func Wave(i int, k, speed, t float64) float64 {
	if Reduced() {
		return .3
	}
	return .5 + .5*math.Sin(float64(i)*k-t*speed)
}
