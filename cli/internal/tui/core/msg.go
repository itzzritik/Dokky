package core

import (
	"context"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
)

type Click struct{ ID string }

type Toast struct {
	Text string
	Bad  bool
}

type OpenWindow struct{ Screen Screen }

// closes that window wherever it is in the stack
type CloseWindow struct{ Screen Screen }

type SwitchTab struct{ Tab int }

type Progress api.AddProgress

// the daemon came back; screens fetch what they keep themselves
type Reconnected struct{}

// ask the app to run the health checks again
type RunChecks struct{}

type Req uint64

var reqs atomic.Uint64

func NextReq() Req { return Req(reqs.Add(1)) }

type Reply struct {
	Req   Req
	Epoch uint64
	Val   any
	Err   error
}

func Send(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func Fail(err error) tea.Cmd { return Send(Toast{Text: Safe(err.Error()), Bad: true}) }

func Say(text string) tea.Cmd { return Send(Toast{Text: text}) }

func Close(s Screen) tea.Cmd { return Send(CloseWindow{Screen: s}) }

func Open(s Screen) tea.Cmd { return Send(OpenWindow{Screen: s}) }

// runs f off the update loop; the app drops the reply if the daemon reconnected meanwhile
func Do(st *State, req Req, f func(context.Context, *client.Client) (any, error)) tea.Cmd {
	c, epoch := st.C, st.Epoch
	return func() tea.Msg {
		v, err := f(context.Background(), c)
		return Reply{Req: req, Epoch: epoch, Val: v, Err: err}
	}
}

// like Do with a context the caller can cancel
func DoCtx(ctx context.Context, st *State, req Req, f func(context.Context, *client.Client) (any, error)) tea.Cmd {
	c, epoch := st.C, st.Epoch
	return func() tea.Msg {
		v, err := f(ctx, c)
		return Reply{Req: req, Epoch: epoch, Val: v, Err: err}
	}
}

type Flight map[string]bool

func (f Flight) Start(key string) bool {
	if f[key] {
		return false
	}
	f[key] = true
	return true
}

func (f Flight) Done(key string) { delete(f, key) }
