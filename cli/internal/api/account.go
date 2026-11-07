package api

import "time"

type Account struct {
	LoggedIn   bool      `json:"loggedIn"`
	Pending    bool      `json:"pending"`
	Offline    bool      `json:"offline"`
	SignedOut  bool      `json:"signedOut"`
	Name       string    `json:"name"`
	Email      string    `json:"email"`
	LastSyncAt time.Time `json:"lastSyncAt"`
}

type State struct {
	Version    string   `json:"version"`
	Build      string   `json:"build,omitempty"`
	Account    Account  `json:"account"`
	Drives     []Drive  `json:"drives"`
	Clashes    []Clash  `json:"clashes"`
	CacheBytes int64    `json:"cacheBytes"`
	CacheDir   string   `json:"cacheDir"`
	Settings   Settings `json:"settings"`
	Removed    *Removed `json:"removed,omitempty"`
}

// this device was removed from the account and is leaving
type Removed struct {
	Mode     string  `json:"mode"`
	Waiting  string  `json:"waiting"`
	Progress float64 `json:"progress"`
}

type Settings struct {
	AutoUpdate       bool   `json:"autoUpdate"`
	PinnedVersion    string `json:"pinnedVersion"`
	NotifyDisconnect bool   `json:"notifyDisconnect"`
	Chosen           bool   `json:"chosen"`
}

type Clash struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Local   string `json:"local"`
	Account string `json:"account"`
}

type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	OS         string    `json:"os"`
	OSName     string    `json:"osName"`
	Arch       string    `json:"arch"`
	Version    string    `json:"version"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	CreatedAt  time.Time `json:"createdAt"`
	Current    bool      `json:"current"`
	Drives     int       `json:"drives"`
	Mounted    []string  `json:"mounted"`
}
