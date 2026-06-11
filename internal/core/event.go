package core

import "time"

// Event is an immutable fact about the environment.
// Payload values are short machine-generated fields only (see security
// posture in docs/PROJECT.md §9) — never free-form external text.
type Event struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Source    string            `json:"source"`
	Payload   map[string]string `json:"payload,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}
