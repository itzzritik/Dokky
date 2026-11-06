package api

import "time"

type DriveState string

const (
	Connected  DriveState = "connected"
	Connecting DriveState = "connecting"
	Off        DriveState = "off"
	Failed     DriveState = "failed"
	Removed    DriveState = "removed"
	Reconnect  DriveState = "reconnect"
)

type Space struct {
	Free  int64 `json:"free"`
	Total int64 `json:"total"`
}

type Drive struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	ServiceID     string     `json:"serviceId"`
	ServiceType   string     `json:"serviceType"`
	ServiceName   string     `json:"serviceName"`
	Account       string     `json:"account"`
	State         DriveState `json:"state"`
	Error         string     `json:"error"`
	Folder        string     `json:"folder"`
	AtLogin       bool       `json:"atLogin"`
	AddedAt       time.Time  `json:"addedAt"`
	CacheBytes    int64      `json:"cacheBytes"`
	Uploads       int        `json:"uploads"`
	CacheMaxSize  string     `json:"cacheMaxSize"`
	CacheMaxAge   string     `json:"cacheMaxAge"`
	SubPath       string     `json:"subPath"`
	LocalFolder   string     `json:"localFolder"`
	Space         *Space     `json:"space"`
	AccountDrives int        `json:"accountDrives"`
	AlsoOn        []string   `json:"alsoOn"`
}

type NewDrive struct {
	ServiceID string        `json:"serviceId"`
	Service   *ServiceDraft `json:"service"`
	Name      string        `json:"name"`
	SubPath   string        `json:"subPath"`
	AtLogin   bool          `json:"atLogin"`
}

type ServiceDraft struct {
	Type    string            `json:"type"`
	Name    string            `json:"name"`
	Options map[string]string `json:"options"`
}

type DriveUpdate struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	SubPath      *string `json:"subPath"`
	CacheMaxSize string  `json:"cacheMaxSize"`
	CacheMaxAge  string  `json:"cacheMaxAge"`
	LocalFolder  *string `json:"localFolder"`
	AtLogin      *bool   `json:"atLogin"`
}

type DriveRemove struct {
	ID string `json:"id"`
}

type AddProgress struct {
	Step  int    `json:"step"`
	Done  bool   `json:"done"`
	Error string `json:"error"`
}
