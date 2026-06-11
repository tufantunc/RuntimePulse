// Package dispatch executes pending continuations: per-session FIFO,
// supervised adapter runs, results fed back as continuation.* events.
package dispatch

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/adapter"
	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/store"
)

const (
	// resumeTimeout bounds one agent turn; a wedged agent CLI must not
	// pin a session's queue forever.
	resumeTimeout = 30 * time.Minute
	// safetyTick rescans for pending work the wake signal may have
	// missed (worker-exit races). Cheap: one indexed SELECT.
	safetyTick = 5 * time.Second
)

// IngestFunc feeds result events back into the engine (chaining).
type IngestFunc func(core.Event) (store.IngestResult, error)

type Dispatcher struct {
	store    *store.Store
	adapters adapter.Registry
	ingest   IngestFunc

	wake chan struct{}

	mu      sync.Mutex
	ctx     context.Context
	workers map[string]bool // sessionID → worker alive
}

func New(st *store.Store, reg adapter.Registry, ingest IngestFunc) *Dispatcher {
	return &Dispatcher{
		store:    st,
		adapters: reg,
		ingest:   ingest,
		wake:     make(chan struct{}, 1),
		workers:  map[string]bool{},
	}
}

// Wake nudges the dispatcher; safe from any goroutine, never blocks.
func (d *Dispatcher) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Run recovers orphaned work, then dispatches until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	d.mu.Lock()
	d.ctx = ctx
	d.mu.Unlock()

	if n, err := d.store.ResetRunningContinuations(); err != nil {
		log.Printf("dispatch: boot recovery failed: %v", err)
	} else if n > 0 {
		log.Printf("dispatch: recovered %d orphaned continuation(s)", n)
	}

	ticker := time.NewTicker(safetyTick)
	defer ticker.Stop()
	for {
		d.scan(ctx)
		select {
		case <-ctx.Done():
			return
		case <-d.wake:
		case <-ticker.C:
		}
	}
}

// scan spawns a worker for every session that has pending work and no
// live worker. Workers exit when their session's queue drains.
func (d *Dispatcher) scan(ctx context.Context) {
	sessions, err := d.store.PendingSessions()
	if err != nil {
		log.Printf("dispatch: scan: %v", err)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, sessionID := range sessions {
		if d.workers[sessionID] {
			continue
		}
		d.workers[sessionID] = true
		go d.worker(ctx, sessionID)
	}
}

func (d *Dispatcher) worker(ctx context.Context, sessionID string) {
	defer func() {
		d.mu.Lock()
		delete(d.workers, sessionID)
		d.mu.Unlock()
		d.Wake() // close the worker-exit race: rescan after deregistering
	}()
	for ctx.Err() == nil {
		c, ok, err := d.store.NextPendingForSession(sessionID)
		if err != nil {
			log.Printf("dispatch: %s: next: %v", sessionID, err)
			return
		}
		if !ok {
			return // queue drained
		}
		claimed, err := d.store.ClaimContinuation(c.ID)
		if err != nil || !claimed {
			continue // someone else got it or it vanished; re-check queue
		}
		d.execute(ctx, c)
	}
}

// execute runs one claimed continuation through its adapter and records
// the outcome. No store state is held across the adapter run.
func (d *Dispatcher) execute(ctx context.Context, c core.Continuation) {
	sess, found, err := d.store.GetSession(c.SessionID)
	state, command, summary := core.ContinuationFailed, "", ""
	var exitCode *int
	var durationMs int64

	switch {
	case err != nil:
		summary = "session lookup failed: " + err.Error()
	case !found:
		summary = "session " + c.SessionID + " is not registered"
	default:
		ad, ok := d.adapters[sess.Agent]
		if !ok {
			summary = "no adapter for agent " + sess.Agent + " (available: claude)"
			break
		}
		d.store.SetSessionState(sess.SessionID, core.SessionRunning)
		runCtx, cancel := context.WithTimeout(ctx, resumeTimeout)
		res, runErr := ad.Resume(runCtx, sess, c.Prompt)
		cancel()
		d.store.SetSessionState(sess.SessionID, core.SessionWaiting)

		command, summary, durationMs = res.Command, res.OutputSummary, res.DurationMs
		ec := res.ExitCode
		exitCode = &ec
		if runErr != nil {
			summary = "resume failed to start: " + runErr.Error()
		} else if res.ExitCode == 0 {
			state = core.ContinuationCompleted
		}
	}

	if err := d.store.UpdateContinuationResult(c.ID, state, command, exitCode, summary); err != nil {
		log.Printf("dispatch: %s: record result: %v", c.ID, err)
	}
	d.emitResult(c, state, exitCode, durationMs)
}

// emitResult publishes continuation.completed/failed with the rule's
// label as source (spec §4.5) — this is what makes chaining work.
func (d *Dispatcher) emitResult(c core.Continuation, state core.ContinuationState, exitCode *int, durationMs int64) {
	evType := "continuation.failed"
	if state == core.ContinuationCompleted {
		evType = "continuation.completed"
	}
	source := c.Label
	if source == "" {
		source = c.RuleID
	}
	payload := map[string]string{
		"continuationId": c.ID,
		"sessionId":      c.SessionID,
		"durationMs":     strconv.FormatInt(durationMs, 10),
	}
	if exitCode != nil {
		payload["exitCode"] = strconv.Itoa(*exitCode)
	}
	if _, err := d.ingest(core.Event{Type: evType, Source: source, Payload: payload}); err != nil {
		log.Printf("dispatch: emit %s for %s: %v", evType, c.ID, err)
	}
}
