package ipc

import "encoding/json"

const (
	Protocol   = 2
	maxMessage = 8 << 20
)

type Request struct {
	V      int             `json:"v"`
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

type CodedError interface{ Code() string }

type Event struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
}
