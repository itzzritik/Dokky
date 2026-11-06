package core

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
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

// WinFsp is missing on Windows
func NoDriver(err error) bool {
	var e *client.Error
	return errors.As(err, &e) && e.Code == "no_driver"
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

// what m does to a drive in its state
func MountLabel(d api.Drive) string {
	switch d.State {
	case api.Connected:
		return "unmount"
	case api.Off, api.Failed:
		return "mount"
	case api.Reconnect:
		return "remount"
	}
	return ""
}

func ViewLabel() string { return "view in " + FileManager() }

func RunMount(st *State, d api.Drive) tea.Cmd {
	switch d.State {
	case api.Connected:
		return Disconnect(st, d.ID)
	case api.Off, api.Failed:
		return Connect(st, d.ID)
	case api.Reconnect:
		return drive(st, d.ID, doReconnect)
	}
	return nil
}
