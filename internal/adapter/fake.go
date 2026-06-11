package adapter

import (
	"context"
	"sync"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// Fake is a controllable in-memory adapter for dispatcher tests.
type Fake struct {
	mu       sync.Mutex
	calls    []FakeCall
	exitCode int
	err      error
	delay    time.Duration
}

type FakeCall struct {
	SessionID string
	RepoPath  string
	Prompt    string
}

func NewFake() *Fake { return &Fake{} }

func (f *Fake) Name() string    { return "fake" }
func (f *Fake) Validate() error { return nil }

func (f *Fake) SetOutcome(exitCode int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exitCode, f.err = exitCode, err
}

func (f *Fake) SetDelay(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delay = d
}

func (f *Fake) Calls() []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeCall(nil), f.calls...)
}

func (f *Fake) Resume(ctx context.Context, sess core.Session, prompt string) (Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, FakeCall{SessionID: sess.SessionID, RepoPath: sess.RepoPath, Prompt: prompt})
	exitCode, err, delay := f.exitCode, f.err, f.delay
	f.mu.Unlock()

	if delay > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(delay):
		}
	}
	return Result{Command: "fake-resume " + sess.SessionID, ExitCode: exitCode, OutputSummary: "fake"}, err
}
