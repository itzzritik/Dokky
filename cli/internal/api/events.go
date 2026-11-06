package api

import "time"

type Event struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`
	Device string    `json:"device"`
	Target string    `json:"target"`
	Name   string    `json:"name"`
	Reason string    `json:"reason"`
	OK     bool      `json:"ok"`
	At     time.Time `json:"at"`
	Key    string    `json:"key,omitempty"`
}

type Events struct {
	Items  []Event `json:"items"`
	Unseen int     `json:"unseen"`
}
