package core

import "time"

type ContinuationState string

const (
	ContinuationPending   ContinuationState = "pending"
	ContinuationRunning   ContinuationState = "running"
	ContinuationCompleted ContinuationState = "completed"
	ContinuationFailed    ContinuationState = "failed"
)

// Continuation is a resumed execution with injected context — never a push.
// Command/ExitCode/OutputSummary are filled by the dispatcher (next plan).
type Continuation struct {
	ID        string `json:"id"`
	RuleID    string `json:"ruleId"`
	EventID   string `json:"eventId"`
	SessionID string `json:"sessionId"`
	Prompt    string `json:"prompt"`
	// Label is denormalized from the originating rule so that
	// continuation.* result events can carry it as their source even if
	// the rule has since been removed (spec §4.5).
	Label         string            `json:"label,omitempty"`
	State         ContinuationState `json:"state"`
	Command       string            `json:"command,omitempty"`
	ExitCode      *int              `json:"exitCode,omitempty"`
	OutputSummary string            `json:"outputSummary,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}
