package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/store"
)

const spaceTimeout = 10 * time.Second

type Moving struct {
	ID, Name   string
	Up         bool
	Size, Done int64
	ETA        time.Duration
}

type Finished struct {
	ID, Name string
	Up       bool
	Size     int64
	At       time.Time
	Error    string
}

type rcTransfer struct {
	Name         string
	Size, Bytes  int64
	ETA          *float64
	SrcFs, DstFs string
	What         string
	Checked      bool
	Error        string
	CompletedAt  time.Time `json:"completed_at"`
}

func (e *Engine) Space(m store.Mount) (free, total int64, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), spaceTimeout)
	defer cancel()
	fsName := m.ServiceID + ":" + m.RemotePath
	useServerSpace(ctx, fsName)
	var out struct{ Total, Used, Free *int64 }
	if runJob(ctx, "operations/about", map[string]any{"fs": fsName}, &out) != nil || out.Free == nil {
		return 0, 0, false
	}
	switch {
	case out.Total != nil:
		return *out.Free, *out.Total, *out.Total > 0
	case out.Used != nil:
		return *out.Free, *out.Free + *out.Used, *out.Free+*out.Used > 0
	}
	return 0, 0, false
}

func (e *Engine) Sample() (int64, []Moving, error) {
	var out struct {
		Bytes        int64
		Transferring []rcTransfer
	}
	if err := rpc("core/stats", nil, &out); err != nil {
		return 0, nil, err
	}
	live := e.snapshot()
	var moving []Moving
	for _, t := range out.Transferring {
		id, up := owner(live, t)
		if id == "" {
			continue
		}
		m := Moving{ID: id, Name: t.Name, Up: up, Size: t.Size, Done: t.Bytes}
		if t.ETA != nil {
			m.ETA = time.Duration(*t.ETA * float64(time.Second))
		}
		moving = append(moving, m)
	}
	return out.Bytes, moving, nil
}

func (e *Engine) Finished() ([]Finished, error) {
	var out struct{ Transferred []rcTransfer }
	if err := rpc("core/transferred", nil, &out); err != nil {
		return nil, err
	}
	live := e.snapshot()
	var done []Finished
	for _, t := range out.Transferred {
		if t.Checked || t.What != "transferring" {
			continue
		}
		if id, up := owner(live, t); id != "" {
			done = append(done, Finished{ID: id, Name: t.Name, Up: up, Size: t.Size, At: t.CompletedAt, Error: t.Error})
		}
	}
	return done, nil
}

func (e *Engine) Retry(id, name string) error {
	lm, ok := e.snapshot()[id]
	if !ok {
		return fmt.Errorf("%s is not mounted", id)
	}
	var out struct {
		Queue []struct {
			Name string
			ID   int64
		}
	}
	if err := rpc("vfs/queue", map[string]any{"fs": lm.fs}, &out); err != nil {
		return err
	}
	for _, q := range out.Queue {
		if q.Name == name {
			return rpc("vfs/queue-set-expiry", map[string]any{"fs": lm.fs, "id": q.ID, "expiry": 0}, nil)
		}
	}
	return errors.New(name + " is no longer waiting")
}

// uploads go cache → remote, reads remote → cache; a mount's fs wins over another on the same service
func owner(live map[string]liveMount, t rcTransfer) (string, bool) {
	match := func(f string) string {
		var byService string
		for id, lm := range live {
			if f == lm.fs {
				return id
			}
			if byService == "" && strings.HasPrefix(f, lm.serviceID+":") {
				byService = id
			}
		}
		return byService
	}
	if t.DstFs != "" {
		if id := match(t.DstFs); id != "" {
			return id, true
		}
	}
	if t.SrcFs != "" {
		return match(t.SrcFs), false
	}
	return "", false
}
