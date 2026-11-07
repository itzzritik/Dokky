package daemon

import (
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type activityEngine struct {
	*fakeEngine
	moving   []engine.Moving
	finished []engine.Finished
}

func (a *activityEngine) Sample() (int64, []engine.Moving, error)  { return 0, a.moving, nil }
func (a *activityEngine) Finished() ([]engine.Finished, error)     { return a.finished, nil }
func (a *activityEngine) Queue(string) ([]engine.QueueItem, error) { return nil, nil }

func TestEarlierLeavesOutFileStillMoving(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample("m1"))
	d.start()
	d.eng = &activityEngine{
		fakeEngine: f,
		moving:     []engine.Moving{{ID: "m1", Name: "big.bin", Size: 100, Done: 40}},
		finished: []engine.Finished{
			{ID: "m1", Name: "big.bin", Size: 100, At: t0},
			{ID: "m1", Name: "other.bin", Size: 5, At: t0},
		},
	}
	d.observe(t0.Add(time.Second))
	a := d.activity()
	require.Len(t, a.Now, 1)
	assert.Equal(t, "big.bin", a.Now[0].Name)
	require.Len(t, a.Earlier, 1)
	assert.Equal(t, "other.bin", a.Earlier[0].Name)
}

func TestUploadSkippedByServerAddsNoSpeed(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample("m1"))
	d.start()
	eng := &activityEngine{fakeEngine: f}
	d.eng = eng
	d.observe(t0)
	eng.finished = []engine.Finished{{ID: "m1", Name: "same.bin", Up: true, Size: 200 << 20, At: t0.Add(500 * time.Millisecond)}}
	d.observe(t0.Add(time.Second))
	assert.Zero(t, d.activity().Up)
}

func TestFinishedTransferCountsItsLastBytes(t *testing.T) {
	d, f, _ := newTestDaemon(t, sample("m1"))
	d.start()
	eng := &activityEngine{fakeEngine: f, moving: []engine.Moving{{ID: "m1", Name: "a.bin", Up: true, Size: 100, Done: 60}}}
	d.eng = eng
	d.observe(t0)
	eng.moving = nil
	eng.finished = []engine.Finished{{ID: "m1", Name: "a.bin", Up: true, Size: 100, Bytes: 100, At: t0.Add(500 * time.Millisecond)}}
	d.observe(t0.Add(time.Second))
	assert.InDelta(t, 40.0, d.activity().Up, 0.001)
}
