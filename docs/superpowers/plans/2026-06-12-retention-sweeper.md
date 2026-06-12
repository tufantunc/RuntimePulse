# RuntimePulse Retention Sweeper Implementation Plan (Stage 10)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The daemon automatically GCs idle sessions (7d, never when an active rule references them or they're running), terminal continuations (30d) and old events (30d) — hourly + at boot, one transaction, no agent/LLM involvement.

**Architecture:** `Store.Sweep(now, RetentionPolicy)` runs three DELETEs in a single transaction (`internal/store/retention.go`); a `sweepOnce`-based goroutine in the daemon's `Serve` ticks hourly. Fixed policy constants, no flags. Three prunes are independent (no cascade).

**Tech Stack:** Go stdlib; existing store/daemon patterns.

**Spec:** `docs/superpowers/specs/2026-06-12-retention-sweeper-design.md` (approved). Key gates: session delete requires age AND no-active-rule AND not-running; pending/running continuations never touched; `PruneEvents` predicate folded into the tx (the standalone method stays).

**File structure:**

```
internal/store/retention.go        — RetentionPolicy, SweepResult, Sweep
internal/store/retention_test.go   — the behavioral matrix (backdating via s.db in-package)
internal/daemon/daemon.go          — sweeper goroutine in Serve + sweepOnce + constants (modify)
internal/daemon/retention_test.go  — wiring sanity (young session survives sweepOnce)
docs/PROJECT.md, docs/usage/getting-started.md — retention behavior (modify)
```

**Verification honesty:** the time-window matrix is store-level (backdating rows needs in-package `s.db` access); the daemon test only proves wiring (sweepOnce runs, young data survives). Smoke is out of scope per the spec (time-based behavior, no clock injection).

---

### Task 1: `Store.Sweep` — the transactional GC

**Files:**
- Create: `internal/store/retention.go`, `internal/store/retention_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/store/retention_test.go`:

```go
package store

import (
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

var testPolicy = RetentionPolicy{
	SessionIdle:     7 * 24 * time.Hour,
	ContinuationAge: 30 * 24 * time.Hour,
	EventAge:        30 * 24 * time.Hour,
}

// backdateSession rewrites updated_at directly — only tests may do this.
func backdateSession(t *testing.T, s *Store, id string, at time.Time) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE sessions SET updated_at = ? WHERE session_id = ?`, ts(at), id); err != nil {
		t.Fatal(err)
	}
}

func backdateContinuation(t *testing.T, s *Store, id string, at time.Time) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE continuations SET updated_at = ? WHERE id = ?`, ts(at), id); err != nil {
		t.Fatal(err)
	}
}

func registerTestSession(t *testing.T, s *Store, id string) {
	t.Helper()
	if _, err := s.RegisterSession(core.Session{SessionID: id, Agent: "claude", RepoPath: "/tmp"}); err != nil {
		t.Fatal(err)
	}
}

func sessionExists(t *testing.T, s *Store, id string) bool {
	t.Helper()
	_, ok, err := s.GetSession(id)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestSweepSessionMatrix(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	old := now.Add(-8 * 24 * time.Hour) // past the 7d window

	// 1) old + no rule + waiting → DELETED
	registerTestSession(t, s, "old-bare")
	backdateSession(t, s, "old-bare", old)

	// 2) old + ACTIVE rule → KEPT
	registerTestSession(t, s, "old-armed")
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "tcp.available"}, SessionID: "old-armed", PromptTemplate: "go",
	})
	backdateSession(t, s, "old-armed", old)

	// 3) old + RUNNING → KEPT
	registerTestSession(t, s, "old-running")
	if err := s.SetSessionState("old-running", core.SessionRunning); err != nil {
		t.Fatal(err)
	}
	backdateSession(t, s, "old-running", old)

	// 4) young + no rule → KEPT (age gate)
	registerTestSession(t, s, "young-bare")

	// 5) old + EXPIRED rule only → DELETED (expired rule is not active)
	registerTestSession(t, s, "old-expired-rule")
	past := now.Add(-time.Hour)
	expired := core.Rule{
		Selector: core.EventSelector{Type: "tcp.available"}, SessionID: "old-expired-rule", PromptTemplate: "go",
	}
	expired.ExpiresAt = &past
	mustAddRule(t, s, expired)
	backdateSession(t, s, "old-expired-rule", old)

	// 6) old + CONSUMED oneShot rule only → DELETED
	registerTestSession(t, s, "old-consumed-rule")
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "exec.succeeded", Source: "gc-test"},
		SessionID: "old-consumed-rule", PromptTemplate: "go", OneShot: true,
	})
	if _, err := s.Ingest(core.Event{ID: "evt-gc-1", Type: "exec.succeeded", Source: "gc-test", Timestamp: now}); err != nil {
		t.Fatal(err) // consumes the oneShot rule
	}
	backdateSession(t, s, "old-consumed-rule", old)

	res, err := s.Sweep(now, testPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sessions != 3 {
		t.Fatalf("swept %d sessions, want 3 (old-bare, old-expired-rule, old-consumed-rule)", res.Sessions)
	}
	for id, want := range map[string]bool{
		"old-bare": false, "old-armed": true, "old-running": true,
		"young-bare": true, "old-expired-rule": false, "old-consumed-rule": false,
	} {
		if got := sessionExists(t, s, id); got != want {
			t.Errorf("session %s: exists=%v want %v", id, got, want)
		}
	}
}

func TestSweepContinuations(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	old := now.Add(-31 * 24 * time.Hour)

	c1 := seedContinuation(t, s, "sess-a", "evt-a") // pending
	// terminal + old → deleted
	c2 := seedContinuationFor(t, s, "sess-b", "evt-b", "http.available")
	s.ClaimContinuation(c2.ID)
	exit := 0
	s.UpdateContinuationResult(c2.ID, core.ContinuationCompleted, "cmd", &exit, "done")
	backdateContinuation(t, s, c2.ID, old)
	// pending + old → KEPT (never touch non-terminal)
	backdateContinuation(t, s, c1.ID, old)
	// terminal + young → kept
	c3 := seedContinuationFor(t, s, "sess-c", "evt-c", "tcp.available")
	s.ClaimContinuation(c3.ID)
	s.UpdateContinuationResult(c3.ID, core.ContinuationFailed, "cmd", &exit, "boom")

	res, err := s.Sweep(now, testPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if res.Continuations != 1 {
		t.Fatalf("swept %d continuations, want 1", res.Continuations)
	}
	all, _ := s.ListContinuations("")
	ids := map[string]bool{}
	for _, c := range all {
		ids[c.ID] = true
	}
	if !ids[c1.ID] || ids[c2.ID] || !ids[c3.ID] {
		t.Fatalf("wrong survivors: %v", ids)
	}
}

// seedContinuationFor mirrors seedContinuation with a custom event type
// so multiple seeds in one test don't collide on rules/events.
func seedContinuationFor(t *testing.T, s *Store, sessionID, evtID, evType string) core.Continuation {
	t.Helper()
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: evType}, SessionID: sessionID,
		PromptTemplate: "go", Label: "seed-" + sessionID,
	})
	res, err := s.Ingest(core.Event{ID: evtID, Type: evType, Source: "x", Timestamp: time.Now().UTC()})
	if err != nil || len(res.Continuations) != 1 {
		t.Fatalf("seed failed: %v %d", err, len(res.Continuations))
	}
	return res.Continuations[0]
}

func TestSweepEvents(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	if err := s.InsertEvent(core.Event{ID: "evt-old", Type: "x", Source: "s", Timestamp: now.Add(-31 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEvent(core.Event{ID: "evt-new", Type: "x", Source: "s", Timestamp: now}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Sweep(now, testPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if res.Events != 1 {
		t.Fatalf("swept %d events, want 1", res.Events)
	}
}

func TestSweepIsIdempotent(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	registerTestSession(t, s, "old-bare")
	backdateSession(t, s, "old-bare", now.Add(-8*24*time.Hour))
	if _, err := s.Sweep(now, testPolicy); err != nil {
		t.Fatal(err)
	}
	res, err := s.Sweep(now, testPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sessions+res.Continuations+res.Events != 0 {
		t.Fatalf("second sweep must delete nothing: %+v", res)
	}
}
```

Note: `TestSweepContinuations` uses the existing `seedContinuation` helper (continuations_test.go) for the first pending row; the new `seedContinuationFor` avoids event-type collisions for the rest. The first seed's `events`/sessions side effects are irrelevant to the assertion (counts are per-table deltas asserted exactly).

Caution on event ages inside continuation tests: seeded events are young, so they don't disturb `res.Events` (asserted only in TestSweepEvents). `TestSweepContinuations`' sweep may also delete old-bare-style sessions — there are none.

Run: `go test ./internal/store/ -run TestSweep -v` → FAIL (Sweep undefined)

- [ ] **Step 2: Implement**

`internal/store/retention.go`:

```go
package store

import "time"

// RetentionPolicy bounds how long pruned state lives. Windows are fixed
// constants in the daemon (owner decision: no flags, YAGNI).
type RetentionPolicy struct {
	SessionIdle     time.Duration
	ContinuationAge time.Duration
	EventAge        time.Duration
}

// SweepResult reports what one retention sweep removed.
type SweepResult struct {
	Sessions      int64
	Continuations int64
	Events        int64
}

// Sweep garbage-collects expired state in ONE transaction:
//
//   - sessions idle past SessionIdle — but never one that is running or
//     referenced by an active (unconsumed, unexpired) rule: an armed
//     session must survive until its rule fires or expires;
//   - terminal (completed/failed) continuations older than
//     ContinuationAge — pending/running are never touched;
//   - events older than EventAge.
//
// The three prunes are independent by design: deleting a session does
// not cascade into its historical continuations (those age out on
// their own and are inert once terminal).
func (s *Store) Sweep(now time.Time, p RetentionPolicy) (SweepResult, error) {
	var res SweepResult
	tx, err := s.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	nowTS := ts(now)

	r, err := tx.Exec(`
		DELETE FROM sessions
		WHERE updated_at < ?
		  AND state != 'running'
		  AND session_id NOT IN (
		    SELECT session_id FROM rules
		    WHERE consumed = 0 AND (expires_at IS NULL OR expires_at > ?)
		  )`,
		ts(now.Add(-p.SessionIdle)), nowTS)
	if err != nil {
		return res, err
	}
	if res.Sessions, err = r.RowsAffected(); err != nil {
		return res, err
	}

	r, err = tx.Exec(`
		DELETE FROM continuations
		WHERE state IN ('completed','failed') AND updated_at < ?`,
		ts(now.Add(-p.ContinuationAge)))
	if err != nil {
		return res, err
	}
	if res.Continuations, err = r.RowsAffected(); err != nil {
		return res, err
	}

	r, err = tx.Exec(`DELETE FROM events WHERE created_at < ?`, ts(now.Add(-p.EventAge)))
	if err != nil {
		return res, err
	}
	if res.Events, err = r.RowsAffected(); err != nil {
		return res, err
	}

	return res, tx.Commit()
}
```

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/store/ -v && go vet ./... && gofmt -l .`
Windows gate: `GOOS=windows go build ./...`
Expected: all green

- [ ] **Step 4: Commit**

```bash
git add internal/store/
git commit -m "feat(store): transactional retention sweep for sessions, continuations and events"
```

---

### Task 2: Daemon sweeper + docs

**Files:**
- Modify: `internal/daemon/daemon.go`
- Create: `internal/daemon/retention_test.go`
- Modify: `docs/PROJECT.md` (§8 Storage), `docs/usage/getting-started.md`

- [ ] **Step 1: Write the failing test**

`internal/daemon/retention_test.go`:

```go
package daemon

import (
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// The time-window matrix lives in the store tests (backdating rows
// requires in-package db access). Here we only prove the wiring: the
// daemon's sweep runs against the real policy and never touches young
// state.
func TestSweepOnceKeepsYoungState(t *testing.T) {
	d, _ := startTestDaemon(t)
	if _, err := d.Engine.Store.RegisterSession(core.Session{
		SessionID: "fresh", Agent: "claude", RepoPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	d.sweepOnce()
	if _, ok, err := d.Engine.Store.GetSession("fresh"); err != nil || !ok {
		t.Fatalf("young session must survive a sweep: ok=%v err=%v", ok, err)
	}
}
```

Run: `go test ./internal/daemon/ -run TestSweepOnce -v` → FAIL (sweepOnce undefined)

- [ ] **Step 2: Implement the sweeper**

In `internal/daemon/daemon.go`:

Constants (near `Version`):

```go
// Retention windows (owner decision: fixed, no flags). The sweeper
// removes idle sessions only when no active rule references them and
// they are not running; terminal continuations and events age out.
const (
	sessionIdleRetention  = 7 * 24 * time.Hour
	continuationRetention = 30 * 24 * time.Hour
	eventRetention        = 30 * 24 * time.Hour
	sweepInterval         = time.Hour
)
```

Method:

```go
// sweepOnce runs one retention sweep; errors are logged, never fatal
// (the next tick retries).
func (d *Daemon) sweepOnce() {
	res, err := d.store.Sweep(time.Now().UTC(), store.RetentionPolicy{
		SessionIdle:     sessionIdleRetention,
		ContinuationAge: continuationRetention,
		EventAge:        eventRetention,
	})
	if err != nil {
		log.Printf("retention sweep failed: %v", err)
		return
	}
	if res.Sessions+res.Continuations+res.Events > 0 {
		log.Printf("retention sweep: sessions=%d continuations=%d events=%d",
			res.Sessions, res.Continuations, res.Events)
	}
}
```

In `Serve`, after the dispatch goroutine launch:

```go
	go func() {
		d.sweepOnce() // boot sweep
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.sweepOnce()
			}
		}
	}()
```

(No shutdown drain needed: a sweep is one quick indexed transaction; `Close()` is unaffected.)

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/daemon/ ./internal/store/ -v && go test ./... > /dev/null && go vet ./... && gofmt -l .`
Windows gate: `GOOS=windows go build ./... && GOOS=windows go vet ./...`
Smoke: `./scripts/smoke.sh` → `SMOKE OK` (the boot sweep must not disturb the smoke's young state — it won't; all smoke data is seconds old)

- [ ] **Step 4: Docs**

* `docs/PROJECT.md` §8 (Storage): replace the aspirational "Events are pruned automatically (configurable retention, default 30 days or size cap)" with the implemented behavior: "A retention sweep runs hourly in the daemon (and at boot): idle sessions are removed after 7 days — never while running or referenced by an active rule — terminal continuations after 30 days, events after 30 days."
* `docs/usage/getting-started.md` (Inspecting state section, one added line): "Old state cleans itself up: the daemon hourly prunes sessions idle for 7+ days (unless an active rule references them), terminal continuations and events older than 30 days — no manual `session rm` needed."

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/ docs/
git commit -m "feat(daemon): hourly retention sweep wires session/continuation/event GC"
```

---

## Self-Review Notes

- **Spec coverage:** triple session gate (age+no-active-rule+not-running) → Task 1 SQL + matrix test rows 1–3; empty-but-young kept → row 4; expired/consumed rules don't protect → rows 5–6; terminal-only continuation pruning → TestSweepContinuations (pending+old kept); PruneEvents predicate in-tx → events DELETE; one transaction → single tx in Sweep; hourly+boot sweeper, log-on-removal, never-fatal → Task 2; fixed constants → Task 2; no cascade → independent DELETEs + spec comment; docs → Task 2 Step 4.
- **Type consistency:** `RetentionPolicy`/`SweepResult`/`Sweep` (Task 1) match daemon usage (Task 2); `sweepOnce` is package-private, callable from the daemon test (same package); helpers reuse existing `mustAddRule`/`seedContinuation`/`openTestStore`/`ts`.
- **Single-connection discipline:** all three DELETEs run on the one tx; no other store call overlaps; the sub-SELECT runs inside the same tx.
- **Honesty:** daemon test proves wiring only; the behavioral matrix is store-level by necessity (backdating). Smoke unaffected and not extended (time-based feature).
```
