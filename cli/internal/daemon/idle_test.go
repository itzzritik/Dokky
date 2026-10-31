package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func TestIdleAfterQuietWindow(t *testing.T) {
	var i Idle
	i.Observe(t0, 0, 100)
	require.False(t, i.IsIdle(t0.Add(idleAfter-time.Second)))
	i.Observe(t0.Add(idleAfter), 0, 100)
	require.True(t, i.IsIdle(t0.Add(idleAfter)))
}

func TestIdleByteChangeResets(t *testing.T) {
	var i Idle
	i.Observe(t0, 0, 100)
	i.Observe(t0.Add(9*time.Minute), 0, 101)
	require.False(t, i.IsIdle(t0.Add(idleAfter)))
	require.True(t, i.IsIdle(t0.Add(9*time.Minute+idleAfter)))
}

func TestIdleNeverWithUploads(t *testing.T) {
	var i Idle
	i.Observe(t0, 0, 100)
	i.Observe(t0.Add(idleAfter), 1, 100)
	require.False(t, i.IsIdle(t0.Add(idleAfter)))
	require.False(t, i.IsIdle(t0.Add(3*idleAfter)))
	i.Observe(t0.Add(3*idleAfter), 0, 100)
	require.False(t, i.IsIdle(t0.Add(3*idleAfter)))
	require.True(t, i.IsIdle(t0.Add(4*idleAfter)))
}

func TestIdleNeverObservedIsNotIdle(t *testing.T) {
	var i Idle
	require.False(t, i.IsIdle(t0.Add(24*time.Hour)))
}

func TestObserveIdleTotalBytesErrorIsNotQuiet(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample())
	d.observeIdle(t0, nil)
	f.failTotal = errors.New("rpc down")
	d.observeIdle(t0.Add(5*time.Minute), nil)
	require.False(t, d.idle.IsIdle(t0.Add(idleAfter+time.Minute)))

	f.failTotal = nil
	recovered := t0.Add(6 * time.Minute)
	d.observeIdle(recovered, nil)
	require.False(t, d.idle.IsIdle(t0.Add(idleAfter+time.Minute)))
	require.True(t, d.idle.IsIdle(recovered.Add(idleAfter)))
}

func TestStatsErrorIsNotQuiet(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample())
	d.observeIdle(t0, nil)
	f.failStats = errors.New("stats down")
	d.observeIdle(t0.Add(5*time.Minute), d.refresh())
	require.False(t, d.idle.IsIdle(t0.Add(idleAfter+time.Minute)))

	f.failStats = nil
	recovered := t0.Add(6 * time.Minute)
	d.observeIdle(recovered, d.refresh())
	require.False(t, d.idle.IsIdle(t0.Add(idleAfter+time.Minute)))
	require.True(t, d.idle.IsIdle(recovered.Add(idleAfter)))
}

func TestTickFeedsIdleUploads(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample("m1"))
	d.start()
	f.uploads = 2
	d.tick(t.Context())
	require.False(t, d.idle.IsIdle(time.Now().Add(24*time.Hour)))
}
