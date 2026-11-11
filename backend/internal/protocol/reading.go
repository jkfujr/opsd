package protocol

import "time"

type ReadStatus struct {
	State       string     `json:"state"`
	DataAt      *time.Time `json:"data_at,omitempty"`
	AttemptedAt *time.Time `json:"attempted_at,omitempty"`
	Error       *Error     `json:"error,omitempty"`
}
