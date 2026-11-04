package store

import "time"

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
	UpdatedAt  time.Time `json:"updatedAt"`
}

type LocalMount struct {
	MountPoint      string `json:"mountPoint"`
	Mounted         bool   `json:"mounted"`
	RemovedRemotely bool   `json:"removedRemotely"`
}

type Settings struct {
	AutoUpdate    bool   `json:"autoUpdate"`
	PinnedVersion string `json:"pinnedVersion"`
}

type Account struct {
	APIBase  string `json:"apiBase"`
	DeviceID string `json:"deviceId"`
	Token    string `json:"token"`
	Rev      string `json:"rev"`
	Pending  bool   `json:"pending"`
}

type Data struct {
	Services []Service             `json:"services"`
	Mounts   []Mount               `json:"mounts"`
	Local    map[string]LocalMount `json:"local"`
	Settings Settings              `json:"settings"`
	Account  *Account              `json:"account"`
}

func Fresh() Data {
	return Data{Local: map[string]LocalMount{}, Settings: Settings{AutoUpdate: true}}
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
