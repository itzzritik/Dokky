package core

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/platform"
)

type DriveDone struct {
	ID  string
	Err error
}

const (
	doConnect = iota
	doDisconnect
	doReconnect
)

// one request per drive at a time; the app clears the flight on DriveDone
func drive(st *State, id string, what int) tea.Cmd {
	if st.Busy == nil {
		st.Busy = Flight{}
	}
	if !st.Busy.Start(id) {
		return nil
	}
	c := st.C
	return func() tea.Msg {
		ctx := context.Background()
		var err error
		if what != doConnect {
			_, err = c.Disconnect(ctx, id)
		}
		if err == nil && what != doDisconnect {
			_, err = c.Connect(ctx, id)
		}
		return DriveDone{ID: id, Err: err}
	}
}

func Connect(st *State, id string) tea.Cmd    { return drive(st, id, doConnect) }
func Disconnect(st *State, id string) tea.Cmd { return drive(st, id, doDisconnect) }

func OpenFolder(path string) tea.Cmd {
	return func() tea.Msg {
		if err := platform.OpenURL(path); err != nil {
			return Toast{Text: "Could not open " + FileManager() + ".", Bad: true}
		}
		return nil
	}
}

// the state's own action: open, connect, remove here or reconnect
func Primary(d api.Drive) string {
	switch d.State {
	case api.Connected:
		return "open"
	case api.Off, api.Failed:
		return "connect"
	case api.Removed:
		return "remove here"
	case api.Reconnect:
		return "reconnect"
	}
	return ""
}

func RunPrimary(st *State, d api.Drive) tea.Cmd {
	switch d.State {
	case api.Connected:
		return OpenFolder(d.Folder)
	case api.Off, api.Failed:
		return Connect(st, d.ID)
	case api.Removed:
		return Disconnect(st, d.ID)
	case api.Reconnect:
		return drive(st, d.ID, doReconnect)
	}
	return nil
}
