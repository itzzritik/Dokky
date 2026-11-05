package api

import "time"

// samples kept, one a second
const HistoryLen = 120

type Sample struct {
	At   time.Time `json:"at"`
	Down float64   `json:"down"`
	Up   float64   `json:"up"`
}

type Transfer struct {
	DriveID string        `json:"driveId"`
	Name    string        `json:"name"`
	Up      bool          `json:"up"`
	Size    int64         `json:"size"`
	Done    int64         `json:"done"`
	ETA     time.Duration `json:"eta"`
	Waiting bool          `json:"waiting"`
	At      time.Time     `json:"at"`
	Error   string        `json:"error"`
	RetryAt time.Time     `json:"retryAt"`
}

type Activity struct {
	Down    float64             `json:"down"`
	Up      float64             `json:"up"`
	History []Sample            `json:"history"`
	ByDrive map[string][]Sample `json:"byDrive"`
	Now     []Transfer          `json:"now"`
	Earlier []Transfer          `json:"earlier"`
}
