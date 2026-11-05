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
	Doctor    func(context.Context) []Check
	Uninstall func() error
}

type Tone int

const (
	Good Tone = iota
	Warn
	Bad
	Busy
)

// one line of the health page
type Check struct {
	Name, Status, Detail string
	Tone                 Tone
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
	Checks    []Check
	Checked   time.Time
	Checking  bool
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

// problems across drives and the last health checks; bad when something is broken, not just worth a look
func (s *State) Problems() (n int, bad bool) {
	for _, d := range s.Snap.Drives {
		if d.State == api.Failed {
			n, bad = n+1, true
		}
	}
	for _, c := range s.Checks {
		switch c.Tone {
		case Bad:
			n, bad = n+1, true
		case Warn:
			n++
		}
	}
	return n, bad
}
