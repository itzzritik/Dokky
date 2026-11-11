package lockwatch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDedupe(t *testing.T) {
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	calls := 0
	once := dedupe(func() { calls++ }, func() time.Time { return at })
	steps := []struct {
		after time.Duration
		want  int
	}{
		{0, 1},
		{0, 1},                      // a lid close: the lock and the sleep together
		{900 * time.Millisecond, 1}, // still within the second
		{100 * time.Millisecond, 2}, // a second after the first
		{5 * time.Second, 3},
		{-10 * time.Second, 3}, // a clock set back is not a new event
	}
	for i, s := range steps {
		at = at.Add(s.after)
		once()
		require.Equal(t, s.want, calls, "step %d", i)
	}
}

func TestSlept(t *testing.T) {
	cases := map[string]struct {
		wall, mono time.Duration
		slept      bool
	}{
		"awake":                       {10 * time.Second, 10 * time.Second, false},
		"a busy tick runs late":       {40 * time.Second, 40 * time.Second, false},
		"a small clock step":          {35 * time.Second, 10 * time.Second, false},
		"asleep for an hour":          {time.Hour, 10 * time.Second, true},
		"the wall clock was set back": {-time.Hour, 10 * time.Second, false},
		"just over the gap":           {41 * time.Second, 10 * time.Second, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, c.slept, slept(c.wall, c.mono))
		})
	}
}

func TestWatchStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Watch(ctx, func() {})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Watch kept running after its context ended")
	}
}
