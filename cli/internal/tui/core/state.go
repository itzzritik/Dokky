package core

import (
	"context"
	"runtime"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
)

type Deps struct {
	Start     func() error
	Doctor    func(context.Context) []string
	Uninstall func() error
}

type State struct {
	Snap      api.State
	Act       api.Activity
	Catalog   []api.Provider
	W, H      int
	Wide      bool
	Connected bool
	Now       time.Time
	T         float64 // seconds on the shared clock
	Epoch     uint64
	C         *client.Client
	Deps      Deps
	Busy      Flight
}

func (s *State) Drive(id string) (api.Drive, bool) {
	for _, d := range s.Snap.Drives {
		if d.ID == id {
			return d, true
		}
	}
	return api.Drive{}, false
}

func FileManager() string {
	switch runtime.GOOS {
	case "darwin":
		return "Finder"
	case "windows":
		return "File Explorer"
	}
	return "Files"
}
