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
	Account    Account  `json:"account"`
	Drives     []Drive  `json:"drives"`
	Clashes    []Clash  `json:"clashes"`
	CacheBytes int64    `json:"cacheBytes"`
	CacheDir   string   `json:"cacheDir"`
	Settings   Settings `json:"settings"`
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
	Arch       string    `json:"arch"`
	Version    string    `json:"version"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	CreatedAt  time.Time `json:"createdAt"`
	Current    bool      `json:"current"`
	Drives     int       `json:"drives"`
}
