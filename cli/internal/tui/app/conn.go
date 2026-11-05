package app

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

const redialEvery = 2 * time.Second

type connected struct {
	c      *client.Client
	snap   api.State
	act    api.Activity
	cat    []api.Provider
	events <-chan client.Event
	stop   context.CancelFunc
}

type lost struct {
	epoch uint64
	err   error
}

type redial struct{}

type event struct {
	ev    client.Event
	epoch uint64
}

type started struct{ err error }

func dial() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		c, err := client.Dial(ctx)
		if err != nil {
			return lost{err: err}
		}
		snap, err := c.State(ctx)
		if err != nil {
			return lost{err: err}
		}
		act, _ := c.Activity(ctx)
		cat, _ := c.Catalog(ctx)
		evCtx, stop := context.WithCancel(context.Background())
		events, err := c.Events(evCtx)
		if err != nil {
			stop()
			return lost{err: err}
		}
		return connected{c: c, snap: snap, act: act, cat: cat, events: events, stop: stop}
	}
}

func (a *App) next() tea.Cmd {
	ch, epoch := a.events, a.st.Epoch
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return lost{epoch: epoch}
		}
		return event{ev, epoch}
	}
}

func (a *App) onConnected(m connected) tea.Cmd {
	if a.stop != nil {
		a.stop()
	}
	a.st.C, a.events, a.stop = m.c, m.events, m.stop
	a.st.Epoch++
	a.st.Connected, a.starting, a.dialing = true, false, false
	a.st.Act, a.st.Catalog = m.act, m.cat
	return tea.Batch(a.setSnap(m.snap), a.next(), a.broadcast(core.Reconnected{}))
}

// a reply from before a reconnect, or an event from an old stream, is dropped
func (a *App) onLost(m lost) tea.Cmd {
	if m.epoch != 0 && m.epoch != a.st.Epoch {
		return nil
	}
	if a.stop != nil {
		a.stop()
		a.stop = nil
	}
	a.st.Connected = false
	if a.dialing {
		return nil
	}
	a.dialing = true
	return tea.Tick(redialEvery, func(time.Time) tea.Msg { return redial{} })
}

func (a *App) onEvent(m event) tea.Cmd {
	if m.epoch != a.st.Epoch {
		return nil
	}
	var cmd tea.Cmd
	switch {
	case m.ev.State != nil:
		cmd = a.setSnap(*m.ev.State)
	case m.ev.Activity != nil:
		a.st.Act = *m.ev.Activity
	case m.ev.Progress != nil:
		cmd = a.broadcast(core.Progress(*m.ev.Progress))
	}
	return tea.Batch(cmd, a.next())
}

func notRunning(err error) bool { return errors.Is(err, client.ErrNotRunning) }

func (a *App) startDaemon() tea.Cmd {
	if a.starting || a.deps.Start == nil {
		return nil
	}
	a.starting = true
	start := a.deps.Start
	return func() tea.Msg { return started{start()} }
}
