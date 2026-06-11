// Package adapter resumes agent sessions via their CLIs (spec §6).
// Adapters are resume-based, never push-based.
package adapter

import (
	"context"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// Result is what supervision records about one resume run.
type Result struct {
	Command       string // the command line that ran (for the continuation record)
	ExitCode      int
	OutputSummary string
	DurationMs    int64
}

// Adapter resumes a session with an injected prompt and reports the
// outcome. Resume returns an error only when the run could not even be
// attempted (binary missing, spawn failure); a nonzero agent exit is a
// Result, not an error.
type Adapter interface {
	Name() string
	Resume(ctx context.Context, sess core.Session, prompt string) (Result, error)
	Validate() error
}

// Registry maps agent names (core.Session.Agent) to adapters.
type Registry map[string]Adapter
