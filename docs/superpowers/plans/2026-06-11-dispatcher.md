# RuntimePulse Dispatcher & Adapters Implementation Plan (Stage 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pending continuations actually resume agent sessions: a supervised dispatcher with per-session FIFO queues executes `claude --resume <id> -p "<prompt>"` in the session's repo, records results, and feeds `continuation.completed/failed` back into the event bus — making multi-step chaining real.

**Architecture:** `internal/adapter` defines the `Adapter` interface with a Claude Code implementation (binary overridable via `RUNTIMEPULSE_CLAUDE_BIN` for hermetic tests) and an in-package `Fake` for dispatcher tests. `internal/dispatch` owns per-session workers: claim (DB-guarded pending→running), resume via adapter with timeout, record result, emit result event through `engine.Ingest` (which may match rules → chaining). The engine gains a `Notify` hook so ingests with continuations wake the dispatcher; a safety tick covers races. Boot recovery resets orphaned `running` → `pending` (at-least-once). Continuations are denormalized with the rule's `label` (becomes the result event's `source` per spec §4.5).

**Decisions binding this plan:**
- **Do NOT re-render prompts** — they were rendered at ingest and stored on the continuation (stage-1 review decision).
- **No permission flags in v1** (owner decision): the resume command carries no `--dangerously-skip-permissions`/`--allowedTools`; the agent runs with its project-configured permissions. Documented limitation: headless runs deny unapproved tools.
- Claude Code adapter only; Cursor/Codex/OpenCode are Phase 3 (spec §13). Unknown agent → continuation fails with a clear message.
- Store has `SetMaxOpenConns(1)`: no store call may happen while another store call's tx/rows are open. The dispatcher never holds DB state across an adapter run.

**Tech Stack:** Go, stdlib `os/exec`, existing internal packages.

**File structure:**

```
internal/core/continuation.go     — + Label field (modify)
internal/store/store.go           — migration: label column (modify)
internal/store/ingest.go          — denormalize rule label (modify)
internal/store/continuations.go   — Claim/UpdateResult/ResetRunning/NextPendingForSession/PendingSessions
internal/adapter/adapter.go       — Adapter interface, Result, Registry
internal/adapter/fake.go          — controllable Fake adapter
internal/adapter/claude.go        — Claude Code adapter (pure arg-builder + parser + runner)
internal/dispatch/dispatcher.go   — Dispatcher
internal/engine/engine.go         — + Notify hook (modify)
internal/daemon/daemon.go|rpc.go  — wiring, continuation.run RPC, status counts (modify)
cmd/runtimepulse/cmd_continue.go  — `continue` + `continuations` commands
scripts/smoke.sh                  — mock-claude end-to-end + chaining (modify)
```

---

### Task 1: Label denormalization (core + migration + ingest)

**Files:**
- Modify: `internal/core/continuation.go`, `internal/store/store.go`, `internal/store/ingest.go`
- Test: `internal/store/ingest_test.go` (append)

- [ ] **Step 1: Write the failing test (append to ingest_test.go)**

```go
func TestIngestDenormalizesRuleLabel(t *testing.T) {
	s := openTestStore(t)
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "docker.healthy"}, SessionID: "abc123",
		PromptTemplate: "go", Label: "step-1",
	})
	res := ingestEvent(t, s, "evt-1")
	if len(res.Continuations) != 1 || res.Continuations[0].Label != "step-1" {
		t.Fatalf("continuation must carry the rule label: %#v", res.Continuations)
	}
	// round-trips through the store
	all, err := s.ListContinuations("")
	if err != nil {
		t.Fatal(err)
	}
	if all[0].Label != "step-1" {
		t.Fatalf("label lost in storage: %#v", all[0])
	}
}
```

Run: `go test ./internal/store/ -run TestIngestDenormalizes -v` → FAIL

- [ ] **Step 2: Implement**

`internal/core/continuation.go` — add to the struct after `Prompt`:

```go
	// Label is denormalized from the originating rule so that
	// continuation.* result events can carry it as their source even if
	// the rule has since been removed (spec §4.5).
	Label string `json:"label,omitempty"`
```

`internal/store/store.go` — the `continuations` schema block gains `label TEXT NOT NULL DEFAULT ''` after `prompt`; AND, because existing DBs already created the table, `migrate()` becomes:

```go
func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// Additive migrations for databases created by earlier versions.
	// "duplicate column name" means the column already exists — fine.
	if _, err := s.db.Exec(`ALTER TABLE continuations ADD COLUMN label TEXT NOT NULL DEFAULT ''`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column name") {
		return err
	}
	return nil
}
```

(add `strings` import)

`internal/store/ingest.go` — in the matched-rules loop set `Label: r.Label` on the continuation, add `label` to the INSERT columns/values, and add `label` to `ListContinuations`'s SELECT + scan (after `prompt`).

- [ ] **Step 3: Run tests, verify pass**

Run: `go test ./internal/... -v`
Expected: PASS (all; existing ingest tests unaffected)

- [ ] **Step 4: Commit**

```bash
git add internal/core/ internal/store/
git commit -m "feat(store): denormalize rule label onto continuations with additive migration"
```

---

### Task 2: Continuation lifecycle store methods

**Files:**
- Create: `internal/store/continuations.go`
- Test: `internal/store/continuations_test.go`

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// seedContinuation ingests one event against one rule and returns the continuation.
func seedContinuation(t *testing.T, s *Store, sessionID, evtID string) core.Continuation {
	t.Helper()
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "tcp.available"}, SessionID: sessionID,
		PromptTemplate: "go", Label: "seed",
	})
	res, err := s.Ingest(core.Event{ID: evtID, Type: "tcp.available", Source: "x"})
	if err != nil || len(res.Continuations) != 1 {
		t.Fatalf("seed failed: %v %d", err, len(res.Continuations))
	}
	return res.Continuations[0]
}

func TestClaimContinuation(t *testing.T) {
	s := openTestStore(t)
	c := seedContinuation(t, s, "sess-1", "evt-1")

	ok, err := s.ClaimContinuation(c.ID)
	if err != nil || !ok {
		t.Fatalf("first claim must succeed: ok=%v err=%v", ok, err)
	}
	ok, err = s.ClaimContinuation(c.ID)
	if err != nil || ok {
		t.Fatalf("second claim must be rejected: ok=%v err=%v", ok, err)
	}
	running, _ := s.ListContinuations("running")
	if len(running) != 1 {
		t.Fatalf("claimed continuation must be running: %d", len(running))
	}
}

func TestUpdateContinuationResult(t *testing.T) {
	s := openTestStore(t)
	c := seedContinuation(t, s, "sess-1", "evt-1")
	s.ClaimContinuation(c.ID)

	exit := 0
	if err := s.UpdateContinuationResult(c.ID, core.ContinuationCompleted,
		"claude --resume sess-1 ...", &exit, "did the thing"); err != nil {
		t.Fatal(err)
	}
	done, _ := s.ListContinuations("completed")
	if len(done) != 1 || done[0].Command == "" || done[0].ExitCode == nil || *done[0].ExitCode != 0 ||
		done[0].OutputSummary != "did the thing" {
		t.Fatalf("result not recorded: %#v", done)
	}
}

func TestResetRunningContinuations(t *testing.T) {
	s := openTestStore(t)
	c := seedContinuation(t, s, "sess-1", "evt-1")
	s.ClaimContinuation(c.ID)

	n, err := s.ResetRunningContinuations()
	if err != nil || n != 1 {
		t.Fatalf("reset: n=%d err=%v", n, err)
	}
	pending, _ := s.ListContinuations("pending")
	if len(pending) != 1 {
		t.Fatal("running continuation must be back to pending after reset")
	}
}

func TestNextPendingAndPendingSessions(t *testing.T) {
	s := openTestStore(t)
	c1 := seedContinuation(t, s, "sess-1", "evt-1")
	// second rule+event for another session
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "http.available"}, SessionID: "sess-2", PromptTemplate: "go",
	})
	if _, err := s.Ingest(core.Event{ID: "evt-2", Type: "http.available", Source: "y"}); err != nil {
		t.Fatal(err)
	}

	sessions, err := s.PendingSessions()
	if err != nil || len(sessions) != 2 {
		t.Fatalf("PendingSessions = %v err=%v, want 2", sessions, err)
	}

	next, ok, err := s.NextPendingForSession("sess-1")
	if err != nil || !ok || next.ID != c1.ID {
		t.Fatalf("NextPendingForSession: %#v ok=%v err=%v", next, ok, err)
	}
	if _, ok, _ := s.NextPendingForSession("sess-none"); ok {
		t.Fatal("no pending for unknown session")
	}
}
```

Run: `go test ./internal/store/ -run 'TestClaim|TestUpdateContinuation|TestResetRunning|TestNextPending' -v` → FAIL

- [ ] **Step 2: Implement**

`internal/store/continuations.go`:

```go
package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// ClaimContinuation atomically moves a continuation pending → running.
// Returns false if it was not pending (already claimed or finished) —
// the DB row is the single source of truth for claims.
func (s *Store) ClaimContinuation(id string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE continuations SET state = 'running', updated_at = ? WHERE id = ? AND state = 'pending'`,
		ts(time.Now().UTC()), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// UpdateContinuationResult records a finished run.
func (s *Store) UpdateContinuationResult(id string, state core.ContinuationState,
	command string, exitCode *int, outputSummary string) error {
	var exit any
	if exitCode != nil {
		exit = *exitCode
	}
	_, err := s.db.Exec(
		`UPDATE continuations SET state = ?, command = ?, exit_code = ?, output_summary = ?, updated_at = ?
		 WHERE id = ?`,
		string(state), command, exit, outputSummary, ts(time.Now().UTC()), id)
	return err
}

// ResetRunningContinuations moves orphaned running rows back to pending
// (daemon crashed mid-run). At-least-once: a continuation whose result
// was lost may run twice; that beats a lost wake-up.
func (s *Store) ResetRunningContinuations() (int64, error) {
	res, err := s.db.Exec(
		`UPDATE continuations SET state = 'pending', updated_at = ? WHERE state = 'running'`,
		ts(time.Now().UTC()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PendingSessions returns the distinct session ids that have pending work.
func (s *Store) PendingSessions() ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT session_id FROM continuations WHERE state = 'pending' ORDER BY session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// NextPendingForSession returns the oldest pending continuation for a
// session (FIFO order: created_at, then id).
func (s *Store) NextPendingForSession(sessionID string) (core.Continuation, bool, error) {
	row := s.db.QueryRow(
		`SELECT id, rule_id, event_id, session_id, prompt, label, state, command,
		        exit_code, output_summary, created_at, updated_at
		 FROM continuations WHERE session_id = ? AND state = 'pending'
		 ORDER BY created_at, id LIMIT 1`, sessionID)
	c, err := scanContinuation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Continuation{}, false, nil
		}
		return core.Continuation{}, false, err
	}
	return c, true, nil
}

func scanContinuation(r rowScanner) (core.Continuation, error) {
	var c core.Continuation
	var st, created, updated string
	var exit *int
	if err := r.Scan(&c.ID, &c.RuleID, &c.EventID, &c.SessionID, &c.Prompt, &c.Label,
		&st, &c.Command, &exit, &c.OutputSummary, &created, &updated); err != nil {
		return core.Continuation{}, err
	}
	c.State = core.ContinuationState(st)
	c.ExitCode = exit
	c.CreatedAt, c.UpdatedAt = parseTS(created), parseTS(updated)
	return c, nil
}
```

Refactor `ListContinuations` (in ingest.go) to reuse `scanContinuation` so the column list lives in two places, not three (keep its `label` column added in Task 1 consistent with this scanner's order: label sits after prompt).

- [ ] **Step 3: Run tests, verify pass**

Run: `go test ./internal/store/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/store/
git commit -m "feat(store): continuation claim/result/reset and FIFO queries for the dispatcher"
```

---

### Task 3: Adapter interface + Fake

**Files:**
- Create: `internal/adapter/adapter.go`, `internal/adapter/fake.go`
- Test: `internal/adapter/fake_test.go`

- [ ] **Step 1: Write the failing test**

```go
package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestFakeRecordsCallsAndControlsOutcome(t *testing.T) {
	f := NewFake()
	sess := core.Session{SessionID: "s1", Agent: "fake", RepoPath: "/tmp"}

	res, err := f.Resume(context.Background(), sess, "do it")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("default fake must succeed: %#v %v", res, err)
	}
	f.SetOutcome(7, errors.New("boom"))
	res, err = f.Resume(context.Background(), sess, "again")
	if err == nil || res.ExitCode != 7 {
		t.Fatalf("configured failure not honored: %#v %v", res, err)
	}
	calls := f.Calls()
	if len(calls) != 2 || calls[0].Prompt != "do it" || calls[1].SessionID != "s1" {
		t.Fatalf("calls not recorded: %#v", calls)
	}
}

func TestFakeHonorsContext(t *testing.T) {
	f := NewFake()
	f.SetDelay(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	f.Resume(ctx, core.Session{SessionID: "s"}, "p")
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("fake must return promptly on ctx cancel")
	}
}
```

Run: `go test ./internal/adapter/ -v` → FAIL

- [ ] **Step 2: Implement**

`internal/adapter/adapter.go`:

```go
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
```

`internal/adapter/fake.go`:

```go
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
```

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/adapter/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/
git commit -m "feat(adapter): adapter interface, registry and controllable fake"
```

---

### Task 4: Claude Code adapter

**Files:**
- Create: `internal/adapter/claude.go`
- Test: `internal/adapter/claude_test.go`

Reference: `docs/reference/claude-code.md` (verified resume syntax). v1 passes **no permission flags** (owner decision) — the agent runs with its project-configured permissions; headless runs deny unapproved tools.

- [ ] **Step 1: Write the failing test**

```go
package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestBuildClaudeArgs(t *testing.T) {
	args := BuildClaudeArgs("abc123", "Postgres ready. Continue.")
	want := []string{"--resume", "abc123", "--output-format", "json", "-p", "--", "Postgres ready. Continue."}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestParseClaudeOutput(t *testing.T) {
	sum, structured := ParseClaudeOutput([]byte(`{"result":"Migrations applied.","session_id":"abc","total_cost_usd":0.01}`))
	if !structured || sum != "Migrations applied." {
		t.Fatalf("json parse failed: %q %v", sum, structured)
	}
	sum, structured = ParseClaudeOutput([]byte("plain text error output"))
	if structured || sum != "plain text error output" {
		t.Fatalf("fallback failed: %q %v", sum, structured)
	}
	long := strings.Repeat("x", 2000)
	sum, _ = ParseClaudeOutput([]byte(long))
	if len(sum) != summaryLimit {
		t.Fatalf("summary not truncated: %d", len(sum))
	}
}

// TestClaudeResumeWithMockBinary exercises the real subprocess path
// hermetically via RUNTIMEPULSE_CLAUDE_BIN.
func TestClaudeResumeWithMockBinary(t *testing.T) {
	dir := t.TempDir()
	mock := filepath.Join(dir, "mock-claude")
	script := "#!/bin/sh\necho '{\"result\":\"ok from mock\"}'\n"
	if err := os.WriteFile(mock, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIMEPULSE_CLAUDE_BIN", mock)

	c := Claude{}
	if err := c.Validate(); err != nil {
		t.Fatalf("validate with mock bin: %v", err)
	}
	res, err := c.Resume(context.Background(),
		core.Session{SessionID: "abc", RepoPath: dir}, "go on")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.OutputSummary != "ok from mock" {
		t.Fatalf("unexpected result: %#v", res)
	}
	if !strings.Contains(res.Command, "--resume abc") {
		t.Fatalf("command not recorded: %q", res.Command)
	}
}

func TestClaudeResumeNonzeroExit(t *testing.T) {
	dir := t.TempDir()
	mock := filepath.Join(dir, "mock-claude")
	if err := os.WriteFile(mock, []byte("#!/bin/sh\necho 'denied' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIMEPULSE_CLAUDE_BIN", mock)
	res, err := Claude{}.Resume(context.Background(),
		core.Session{SessionID: "abc", RepoPath: dir}, "go")
	if err != nil {
		t.Fatalf("nonzero exit is a Result, not an error: %v", err)
	}
	if res.ExitCode != 1 || !strings.Contains(res.OutputSummary, "denied") {
		t.Fatalf("failure not captured: %#v", res)
	}
}
```

Run: `go test ./internal/adapter/ -run TestBuildClaude -v` → FAIL

- [ ] **Step 2: Implement**

`internal/adapter/claude.go`:

```go
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

const summaryLimit = 500

// Claude resumes Claude Code sessions headlessly:
//
//	claude --resume <session-id> -p "<prompt>" --output-format json
//
// run in the session's repoPath (restores the original project config —
// see docs/reference/claude-code.md). v1 passes NO permission flags
// (owner decision): the agent runs with its project-configured
// permissions, and unapproved tool calls are denied in headless mode.
type Claude struct{}

func (Claude) Name() string { return "claude" }

func (Claude) Validate() error {
	_, err := exec.LookPath(claudeBin())
	return err
}

// claudeBin allows tests and the smoke script to substitute a mock
// binary; production uses "claude" from PATH.
func claudeBin() string {
	if b := os.Getenv("RUNTIMEPULSE_CLAUDE_BIN"); b != "" {
		return b
	}
	return "claude"
}

func BuildClaudeArgs(sessionID, prompt string) []string {
	return []string{"--resume", sessionID, "--output-format", "json", "-p", "--", prompt}
}

// ParseClaudeOutput extracts the result text from --output-format json,
// falling back to the raw output. The boolean reports whether
// structured output was recognized.
func ParseClaudeOutput(out []byte) (string, bool) {
	var r struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out, &r); err == nil && r.Result != "" {
		return truncate(r.Result), true
	}
	return truncate(strings.TrimSpace(string(out))), false
}

func truncate(s string) string {
	if len(s) > summaryLimit {
		return s[:summaryLimit]
	}
	return s
}

func (c Claude) Resume(ctx context.Context, sess core.Session, prompt string) (Result, error) {
	bin := claudeBin()
	args := BuildClaudeArgs(sess.SessionID, prompt)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = sess.RepoPath

	start := time.Now()
	out, err := cmd.CombinedOutput()
	res := Result{
		Command:    bin + " " + strings.Join(args, " "),
		DurationMs: time.Since(start).Milliseconds(),
	}
	summary, _ := ParseClaudeOutput(out)
	res.OutputSummary = summary

	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode() // agent ran and failed: a Result, not an error
			return res, nil
		}
		return res, err // could not even run (binary missing, spawn failure)
	}
	return res, nil
}
```

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/adapter/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/
git commit -m "feat(adapter): claude code adapter with mockable binary and structured output parsing"
```

---

### Task 5: Dispatcher

**Files:**
- Create: `internal/dispatch/dispatcher.go`
- Test: `internal/dispatch/dispatcher_test.go`

**Design constraints recap:** claim is DB-guarded; no store call overlaps another (single conn — every store method fully materializes before the next call); the adapter run happens with NO store state held; result events go through an injected `ingest` func (the engine), which is what makes chaining work.

- [ ] **Step 1: Write the failing test**

```go
package dispatch

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/adapter"
	"github.com/tufantunc/RuntimePulse/internal/bus"
	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/engine"
	"github.com/tufantunc/RuntimePulse/internal/store"
)

type fixture struct {
	store *store.Store
	eng   *engine.Engine
	fake  *adapter.Fake
	disp  *Dispatcher
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b := bus.New()
	eng := engine.New(st, b)
	fake := adapter.NewFake()
	d := New(st, adapter.Registry{"fake": fake}, eng.Ingest)
	eng.Notify = d.Wake

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.Run(ctx)
	return &fixture{store: st, eng: eng, fake: fake, disp: d}
}

func (f *fixture) registerSession(t *testing.T, id string) {
	t.Helper()
	if _, err := f.store.RegisterSession(core.Session{SessionID: id, Agent: "fake", RepoPath: "/tmp"}); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) addRule(t *testing.T, evType, sessionID, label string) {
	t.Helper()
	if _, err := f.store.AddRule(core.Rule{
		Selector: core.EventSelector{Type: evType}, SessionID: sessionID,
		PromptTemplate: "continue " + sessionID, Label: label, OneShot: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestDispatchCompletesAndEmitsResultEvent(t *testing.T) {
	f := newFixture(t)
	f.registerSession(t, "sess-1")
	f.addRule(t, "tcp.available", "sess-1", "step-1")

	if _, err := f.eng.Ingest(core.Event{Type: "tcp.available", Source: "x"}); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "continuation completed", func() bool {
		done, _ := f.store.ListContinuations("completed")
		return len(done) == 1
	})
	if calls := f.fake.Calls(); len(calls) != 1 || calls[0].Prompt != "continue sess-1" {
		t.Fatalf("adapter not called with stored prompt: %#v", calls)
	}
	// result event with the rule label as source (spec §4.5)
	waitFor(t, "continuation.completed event", func() bool {
		evs, _ := f.store.ListEvents("continuation.completed", 10)
		return len(evs) == 1 && evs[0].Source == "step-1"
	})
}

func TestDispatchFailureEmitsFailedEvent(t *testing.T) {
	f := newFixture(t)
	f.registerSession(t, "sess-1")
	f.addRule(t, "tcp.available", "sess-1", "")
	f.fake.SetOutcome(2, nil) // agent ran, exited 2

	f.eng.Ingest(core.Event{Type: "tcp.available", Source: "x"})
	waitFor(t, "failed continuation", func() bool {
		failed, _ := f.store.ListContinuations("failed")
		return len(failed) == 1
	})
	waitFor(t, "continuation.failed event", func() bool {
		evs, _ := f.store.ListEvents("continuation.failed", 10)
		return len(evs) == 1
	})
}

func TestDispatchChaining(t *testing.T) {
	f := newFixture(t)
	f.registerSession(t, "sess-1")
	f.addRule(t, "tcp.available", "sess-1", "step-1")
	// step-2 fires on step-1's completion — pure rule mechanics
	if _, err := f.store.AddRule(core.Rule{
		Selector: core.EventSelector{Type: "continuation.completed", Source: "step-1"},
		SessionID: "sess-1", PromptTemplate: "step 2", Label: "step-2", OneShot: true,
	}); err != nil {
		t.Fatal(err)
	}

	f.eng.Ingest(core.Event{Type: "tcp.available", Source: "x"})
	waitFor(t, "two completed continuations (chain)", func() bool {
		done, _ := f.store.ListContinuations("completed")
		return len(done) == 2
	})
	calls := f.fake.Calls()
	if len(calls) != 2 || calls[1].Prompt != "step 2" {
		t.Fatalf("chain did not run step 2: %#v", calls)
	}
}

func TestDispatchFIFOWithinSession(t *testing.T) {
	f := newFixture(t)
	f.registerSession(t, "sess-1")
	f.fake.SetDelay(50 * time.Millisecond)
	// two non-oneShot rules on the same event type, same session
	for _, label := range []string{"a", "b"} {
		if _, err := f.store.AddRule(core.Rule{
			Selector: core.EventSelector{Type: "tcp.available"}, SessionID: "sess-1",
			PromptTemplate: "p-" + label, Label: label,
		}); err != nil {
			t.Fatal(err)
		}
	}
	f.eng.Ingest(core.Event{Type: "tcp.available", Source: "x"})
	waitFor(t, "both continuations done", func() bool {
		done, _ := f.store.ListContinuations("completed")
		return len(done) == 2
	})
	calls := f.fake.Calls()
	if len(calls) != 2 {
		t.Fatalf("want 2 serial calls, got %d", len(calls))
	}
}

func TestDispatchUnknownAgentFails(t *testing.T) {
	f := newFixture(t)
	if _, err := f.store.RegisterSession(core.Session{SessionID: "sess-x", Agent: "cursor", RepoPath: "/tmp"}); err != nil {
		t.Fatal(err)
	}
	f.addRule(t, "tcp.available", "sess-x", "")
	f.eng.Ingest(core.Event{Type: "tcp.available", Source: "x"})
	waitFor(t, "unknown-agent failure", func() bool {
		failed, _ := f.store.ListContinuations("failed")
		return len(failed) == 1 && failed[0].OutputSummary != ""
	})
}

func TestBootRecovery(t *testing.T) {
	// store with an orphaned running continuation, dispatcher started after
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := bus.New()
	eng := engine.New(st, b)
	if _, err := st.RegisterSession(core.Session{SessionID: "sess-1", Agent: "fake", RepoPath: "/tmp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddRule(core.Rule{
		Selector: core.EventSelector{Type: "tcp.available"}, SessionID: "sess-1", PromptTemplate: "go",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := st.Ingest(core.Event{ID: "evt-1", Type: "tcp.available", Source: "x", Timestamp: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	st.ClaimContinuation(res.Continuations[0].ID) // simulate crash mid-run

	fake := adapter.NewFake()
	d := New(st, adapter.Registry{"fake": fake}, eng.Ingest)
	eng.Notify = d.Wake
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	waitFor(t, "recovered continuation", func() bool {
		done, _ := st.ListContinuations("completed")
		return len(done) == 1
	})
	_ = errors.New // keep imports tidy if unused
}
```

(If `errors` ends up unused in the final test file, drop the import and the `_ = errors.New` line.)

Run: `go test ./internal/dispatch/ -v` → FAIL

- [ ] **Step 2: Implement**

`internal/dispatch/dispatcher.go`:

```go
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
```

`internal/engine/engine.go` — add the hook. New field + call at the end of `Ingest` (before returning, after Publish):

```go
type Engine struct {
	Store *store.Store
	Bus   *bus.Bus
	// Notify, when set, is called after any ingest that produced
	// continuations — the dispatcher's wake signal.
	Notify func()
}
```

```go
	e.Bus.Publish(res.Event)
	if e.Notify != nil && len(res.Continuations) > 0 {
		e.Notify()
	}
	return res, nil
```

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race -count=2 ./internal/dispatch/ ./internal/engine/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/dispatch/ internal/engine/
git commit -m "feat(dispatch): supervised per-session dispatcher with chaining result events"
```

---

### Task 6: Daemon wiring + continuation RPC

**Files:**
- Modify: `internal/daemon/daemon.go` (dispatcher lifecycle), `internal/daemon/rpc.go` (continuation.run; status counts)
- Test: `internal/daemon/dispatch_rpc_test.go`

- [ ] **Step 1: Write the failing test**

```go
package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mockClaude installs a fake claude binary for the test daemon.
func mockClaude(t *testing.T, script string) {
	t.Helper()
	dir := shortTempDir(t)
	mock := filepath.Join(dir, "mock-claude")
	if err := os.WriteFile(mock, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIMEPULSE_CLAUDE_BIN", mock)
}

func TestContinuationRunRPC(t *testing.T) {
	mockClaude(t, "#!/bin/sh\necho '{\"result\":\"manual run ok\"}'\n")
	_, c := startTestDaemon(t)

	if err := c.Call("session.register",
		map[string]any{"sessionId": "abc", "agent": "claude", "repoPath": "/tmp"}, nil); err != nil {
		t.Fatal(err)
	}
	var cont map[string]any
	if err := c.Call("continuation.run",
		map[string]any{"sessionId": "abc", "prompt": "do the thing"}, &cont); err != nil {
		t.Fatal(err)
	}
	if cont["id"] == "" {
		t.Fatalf("continuation.run returned no id: %v", cont)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		var done []map[string]any
		if err := c.Call("continuation.list", map[string]any{"state": "completed"}, &done); err != nil {
			t.Fatal(err)
		}
		if len(done) == 1 {
			if s, _ := done[0]["outputSummary"].(string); s != "manual run ok" {
				t.Fatalf("summary = %v", done[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("manual continuation never completed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestContinuationRunUnknownSession(t *testing.T) {
	_, c := startTestDaemon(t)
	err := c.Call("continuation.run", map[string]any{"sessionId": "ghost", "prompt": "x"}, nil)
	if err == nil {
		t.Fatal("continuation.run for unregistered session must error")
	}
}
```

Run: `go test ./internal/daemon/ -run TestContinuationRun -v` → FAIL

- [ ] **Step 2: Wire the dispatcher**

`internal/daemon/daemon.go`:
- imports: add `adapter "github.com/tufantunc/RuntimePulse/internal/adapter"`, `"github.com/tufantunc/RuntimePulse/internal/dispatch"`.
- `Daemon` gains field `Dispatch *dispatch.Dispatcher`.
- In `New`, after the watch manager:

```go
	disp := dispatch.New(st, adapter.Registry{"claude": adapter.Claude{}}, eng.Ingest)
	eng.Notify = disp.Wake
```

(populate `Dispatch: disp` in the returned struct)
- In `Serve`, after `d.Watches.Run(ctx, persisted)`:

```go
	go d.Dispatch.Run(ctx)
```

- [ ] **Step 3: RPC additions**

In `dispatch` switch in `rpc.go`:

```go
	case "continuation.run":
		p, err := unmarshalParams[struct {
			SessionID string `json:"sessionId"`
			Prompt    string `json:"prompt"`
		}](params)
		if err != nil {
			return nil, err
		}
		if p.SessionID == "" || p.Prompt == "" {
			return nil, errors.New("continuation.run: sessionId and prompt are required")
		}
		if _, ok, err := d.Engine.Store.GetSession(p.SessionID); err != nil {
			return nil, err
		} else if !ok {
			return nil, fmt.Errorf("continuation.run: unknown session %q", p.SessionID)
		}
		// A manual continuation is rule-less: synthetic unique ids keep
		// the (rule_id, event_id) idempotency constraint satisfied.
		c, err := d.Engine.Store.InsertManualContinuation(p.SessionID, p.Prompt)
		if err != nil {
			return nil, err
		}
		d.Dispatch.Wake()
		return c, nil
```

This needs one more store method — add to `internal/store/continuations.go`:

```go
// InsertManualContinuation creates a rule-less pending continuation
// (the CLI `continue` command / continuation.run RPC).
func (s *Store) InsertManualContinuation(sessionID, prompt string) (core.Continuation, error) {
	now := time.Now().UTC()
	c := core.Continuation{
		ID: core.NewID("cont"), RuleID: core.NewID("manual"), EventID: core.NewID("manual"),
		SessionID: sessionID, Prompt: prompt, Label: "manual",
		State: core.ContinuationPending, CreatedAt: now, UpdatedAt: now,
	}
	_, err := s.db.Exec(`
		INSERT INTO continuations
		  (id, rule_id, event_id, session_id, prompt, label, state, output_summary, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.RuleID, c.EventID, c.SessionID, c.Prompt, c.Label, string(c.State), "", ts(now), ts(now))
	return c, err
}
```

Also extend `status()` with `"runningContinuations"` (count of `ListContinuations("running")`).

- [ ] **Step 4: Run tests, verify pass**

Run: `go test -race ./internal/... -v`
Expected: PASS (all)

- [ ] **Step 5: Commit**

```bash
git add internal/
git commit -m "feat(daemon): dispatcher lifecycle and manual continuation.run RPC"
```

---

### Task 7: CLI — `continue` and `continuations`

**Files:**
- Create: `cmd/runtimepulse/cmd_continue.go`
- Modify: `cmd/runtimepulse/main.go` (register)

- [ ] **Step 1: Implement**

`cmd/runtimepulse/cmd_continue.go`:

```go
package main

import "github.com/spf13/cobra"

// continue: spec §7.1's manual trigger — enqueue a continuation for a
// registered session without waiting for an event.
func continueCmd() *cobra.Command {
	var session, prompt string
	cmd := &cobra.Command{
		Use:   "continue",
		Short: "Manually resume a registered session with a prompt",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var cont map[string]any
			if err := c.Call("continuation.run", map[string]any{
				"sessionId": session, "prompt": prompt,
			}, &cont); err != nil {
				return err
			}
			return printJSON(cont)
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "registered session id (required)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "prompt to inject (required)")
	cmd.MarkFlagRequired("session")
	cmd.MarkFlagRequired("prompt")
	return cmd
}

func continuationsCmd() *cobra.Command {
	var state string
	cmd := &cobra.Command{
		Use:   "continuations",
		Short: "List continuations (JSON lines)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var list []map[string]any
			if err := c.Call("continuation.list", map[string]any{"state": state}, &list); err != nil {
				return err
			}
			for _, item := range list {
				if err := printJSON(item); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "filter: pending|running|completed|failed")
	return cmd
}
```

`main.go`: append `continueCmd(), continuationsCmd()` to `AddCommand`.

- [ ] **Step 2: Verify build + help**

Run: `go build ./... && go run ./cmd/runtimepulse continue --help`
Expected: builds; both commands listed in root help

- [ ] **Step 3: Commit**

```bash
git add cmd/
git commit -m "feat(cli): continue and continuations commands"
```

---

### Task 8: Smoke — end-to-end resume with mock claude + chaining

**Files:**
- Modify: `scripts/smoke.sh`, `docs/PROJECT.md` (§13 progress note)

- [ ] **Step 1: Extend smoke**

At the TOP of the script (after `export RUNTIMEPULSE_DIR`), install a mock claude so every daemon in this script can dispatch:

```bash
cat > "$dir/mock-claude" << 'MOCK'
#!/bin/sh
# stand-in for the claude CLI: prints structured output like
# `claude -p --output-format json` and exits 0
echo '{"result":"mock turn done","session_id":"'$2'"}'
MOCK
chmod +x "$dir/mock-claude"
export RUNTIMEPULSE_CLAUDE_BIN="$dir/mock-claude"
```

Insert before the final `echo "SMOKE OK"`:

```bash
# --- dispatcher: event → resume (mock claude) → chained second step ---
"$rp" session register --agent claude --session smoke-3 --repo "$dir"
"$rp" rule add --on tcp.available:smoke-tcp --session smoke-3 \
  --prompt 'TCP up. Do step one.' --one-shot --label chain-1
"$rp" rule add --on continuation.completed:chain-1 --session smoke-3 \
  --prompt 'Step one done. Do step two.' --one-shot --label chain-2
"$rp" inject --type tcp.available --source smoke-tcp
sleep 1.5
completed=$("$rp" continuations --state completed | grep -c smoke-3)
[ "$completed" -eq 2 ] || { echo "FAIL: chain expected 2 completed continuations for smoke-3, got $completed"; exit 1; }
"$rp" events --type continuation.completed | grep -q chain-2 || { echo "FAIL: chain-2 completion event missing"; exit 1; }

# --- manual continue ---
"$rp" continue --session smoke-3 --prompt "manual poke"
sleep 0.8
"$rp" continuations --state completed | grep -q '"label":"manual"' || { echo "FAIL: manual continuation missing"; exit 1; }
```

Note: earlier smoke sections asserted `pendingContinuations:1` / `:2` — those continuations belong to sessions `smoke-1`/`smoke-2` whose agent is `claude`, so the dispatcher will now pick them up and run them against the mock! Their prompts will complete and pending counts will drop. **Rework those assertions**: after the dispatcher exists, "pendingContinuations" is transient. Change the two earlier greps from `"pendingContinuations":1` / `:2` to instead assert via `continuations` totals:

- First block: replace the two status greps with
  `sleep 1` then `"$rp" continuations | grep -c smoke-1` must equal `1` (oneShot: exactly one continuation ever created, regardless of state), and after the second inject it must still equal `1`.
- File-watch block: `"$rp" continuations | grep -c smoke-2` must equal `1`.

- [ ] **Step 2: PROJECT.md progress note**

In §13, mark items 1–3: prefix `1.`/`2.`/`3.` with ✅ for core, watchers, and "Continuation: dispatcher, per-session queues, supervision, Claude Code adapter" — leave "then Cursor/Codex/OpenCode" unmarked (Phase 3). In §6 (adapters), add one sentence: "v1 passes no permission flags on resume; the agent runs with its project-configured permissions (owner decision, 2026-06-11)."

- [ ] **Step 3: Run everything**

Run: `./scripts/smoke.sh && ./scripts/smoke.sh && go test -race ./... && go vet ./... && gofmt -l .`
Expected: `SMOKE OK` twice, all green

- [ ] **Step 4: Commit**

```bash
git add scripts/smoke.sh docs/PROJECT.md
git commit -m "test: end-to-end dispatch and chaining smoke with mock claude"
```

---

## Self-Review Notes

- **Spec coverage:** supervised execution + result recording (§4.5, §5 steps 3–5) → Tasks 2/5; per-session FIFO (§5) → Task 5 worker; chaining via continuation.* events with label as source (§4.5, memory note) → Tasks 1/5; boot recovery of running work (§5 step 6) → Task 5 Run; `runtimepulse continue` (§7.1) → Tasks 6–7; adapter interface + Validate (§6) → Tasks 3–4; no-re-render decision honored (dispatcher only reads `c.Prompt`).
- **Type consistency:** `store.ClaimContinuation/UpdateContinuationResult/ResetRunningContinuations/PendingSessions/NextPendingForSession/InsertManualContinuation` (Tasks 2/6) match dispatcher usage (Task 5) and RPC (Task 6); `adapter.Registry{"claude": adapter.Claude{}}` matches Task 4's type; `engine.Notify` set in both fixture and daemon.
- **Single-conn audit:** dispatcher store calls are sequential, each fully materializes; no call overlaps the adapter run; `execute` does lookup → claim already done → SetSessionState → (adapter, no DB) → SetSessionState → UpdateResult → ingest (own tx). Safe.
- **Smoke interaction:** adding a live dispatcher changes earlier smoke assertions (pending counts become transient) — Task 8 reworks them deliberately; implementer must not "fix" this by disabling dispatch.
```
