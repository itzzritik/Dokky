package core

import (
	"context"
	"runtime"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
)

type Deps struct {
	Reopened  bool // this run follows a reopen, so repairs that reopen again are left out
	Remote    bool // over SSH: nobody can answer Touch ID or Windows Hello at this computer
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

// one line of the health page: what it means, the facts behind it, and either Fix all's repair or what only the person can do
type Check struct {
	Name, Status, Detail, Fix string
	Facts                     [][2]string
	Tone                      Tone
	Expected                  bool         // normal on this computer, so it stays yellow but isn't counted as a problem
	Auto                      string       // what Fix all does about it
	Repair                    func() error // Fix all's work, off the update loop
	Reopen                    bool         // dokky quits and opens again once repaired
}

type State struct {
	Snap      api.State
	Act       api.Activity
	Log       api.Events // the activity log, kept fresh while dokky is open
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
	Fixing    bool   // Fix all is running
	Updating  string // the update's step for the header, empty when idle
	Latest    string // a newer dokky to update to
}

func (s *State) UpdateReady() bool { return s.Latest != "" && s.Updating == "" }

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

// problems from the last health checks; bad when something is broken, not just worth a look
func (s *State) Problems() (n int, bad bool) {
	for _, c := range s.Checks {
		switch {
		case c.Expected:
		case c.Tone == Bad:
			n, bad = n+1, true
		case c.Tone == Warn:
			n++
		}
	}
	return n, bad
}
