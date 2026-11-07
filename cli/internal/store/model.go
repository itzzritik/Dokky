package store

import (
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
)

type Service struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Options   map[string]string `json:"options"`
	Secrets   map[string]string `json:"secrets"`
	Version   int64             `json:"version"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

type VFS struct {
	CacheMode         string `json:"cacheMode"`
	DirCacheTime      string `json:"dirCacheTime"`
	ReadAhead         string `json:"readAhead"`
	ReadChunkStreams  int    `json:"readChunkStreams"`
	CacheMaxSize      string `json:"cacheMaxSize"`
	CacheMaxAge       string `json:"cacheMaxAge"`
	CachePollInterval string `json:"cachePollInterval"`
	CacheMinFreeSpace string `json:"cacheMinFreeSpace"`
	FastFingerprint   bool   `json:"fastFingerprint"`
}

type Mount struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	ServiceID  string    `json:"serviceId"`
	RemotePath string    `json:"remotePath"`
	VFS        VFS       `json:"vfs"`
	Version    int64     `json:"version"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type LocalMount struct {
	MountPoint   string `json:"mountPoint"`
	Mounted      bool   `json:"mounted"`
	Removing     bool   `json:"removing,omitempty"`
	CacheMaxSize string `json:"cacheMaxSize,omitempty"`
	CacheMaxAge  string `json:"cacheMaxAge,omitempty"`
}

type Settings struct {
	AutoUpdate       bool   `json:"autoUpdate"`
	PinnedVersion    string `json:"pinnedVersion"`
	NotifyDisconnect bool   `json:"notifyDisconnect"`
	Chosen           bool   `json:"chosen"`
}

type Account struct {
	APIBase    string    `json:"apiBase"`
	DeviceID   string    `json:"deviceId"`
	Token      string    `json:"token"`
	Rev        string    `json:"rev"`
	Pending    bool      `json:"pending"`
	Name       string    `json:"name"`
	Email      string    `json:"email"`
	LastSyncAt time.Time `json:"lastSyncAt"`
}

type Data struct {
	Services []Service             `json:"services"`
	Mounts   []Mount               `json:"mounts"`
	Local    map[string]LocalMount `json:"local"`
	Settings Settings              `json:"settings"`
	Account  *Account              `json:"account"`
	Events   EventLog              `json:"events"`
	Removal  *Removal              `json:"removal,omitempty"`
}

// the server removed this device; it is kept until the erase is reported
type Removal struct {
	Mode   string    `json:"mode"`
	Erased bool      `json:"erased"`
	At     time.Time `json:"at"`
}

// Queue waits to reach the server; Local is the log of a device with no account
type EventLog struct {
	Queue  []api.Event `json:"queue,omitempty"`
	Local  []api.Event `json:"local,omitempty"`
	SeenAt time.Time   `json:"seenAt"`
}

func Fresh() Data {
	return Data{Local: map[string]LocalMount{}, Settings: Settings{AutoUpdate: true, NotifyDisconnect: true}}
}

func DefaultVFS() VFS {
	return VFS{
		CacheMode:         "full",
		DirCacheTime:      "5m",
		ReadAhead:         "16M",
		ReadChunkStreams:  0,
		CacheMaxSize:      "50G",
		CacheMaxAge:       "5m",
		CachePollInterval: "10s",
		CacheMinFreeSpace: "10G",
		FastFingerprint:   true,
	}
}
