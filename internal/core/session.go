package core

import "time"

type SessionState string

const (
	SessionWaiting  SessionState = "waiting"
	SessionQueued   SessionState = "queued"
	SessionResuming SessionState = "resuming"
	SessionRunning  SessionState = "running"
	SessionDone     SessionState = "done"
)

// Session is an agent execution context known to the registry.
type Session struct {
	SessionID string       `json:"sessionId"`
	Agent     string       `json:"agent"` // claude | cursor | codex | opencode
	RepoPath  string       `json:"repoPath"`
	State     SessionState `json:"state"`
	CreatedAt time.Time    `json:"createdAt"`
	UpdatedAt time.Time    `json:"updatedAt"`
}
