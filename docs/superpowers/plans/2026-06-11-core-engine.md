# RuntimePulse Core Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build RuntimePulse's core: SQLite store with transactional event ingest (outbox pattern), in-process event bus, rule engine, daemon with unix-socket RPC, and the CLI skeleton — so that an injected event matches rules and produces pending continuations, observable via `status`/`events --follow`.

**Architecture:** A single `runtimepulsed` daemon owns store+bus+engine and serves NDJSON request/response RPC over a 0600 unix socket; the CLI is a thin client that auto-starts the daemon. Event insert, rule matching, prompt rendering, continuation creation, and oneShot consumption happen in one SQLite transaction; `(rule_id, event_id)` uniqueness gives idempotency. The continuation *dispatcher* (running agent CLIs) is the NEXT plan — here continuations stop at `pending`.

**Tech Stack:** Go 1.26, `modernc.org/sqlite` (CGO-free), `github.com/spf13/cobra`, stdlib `text/template`, `encoding/json`, `net` (unix sockets).

**Spec:** `docs/PROJECT.md` (authoritative), rationale in `docs/superpowers/specs/2026-06-11-runtimepulse-architecture-design.md`. Out of scope for this plan: watchers, dispatcher/adapters, MCP server, workflow YAML.

**File structure:**

```
cmd/runtimepulse/           — package main: root + subcommands (thin client)
  main.go cmd_daemon.go cmd_status.go cmd_events.go cmd_rule.go cmd_session.go cmd_inject.go
internal/core/              — pure domain types, no I/O
  id.go event.go rule.go session.go continuation.go
internal/store/             — SQLite persistence
  store.go events.go rules.go sessions.go ingest.go
internal/bus/bus.go         — in-process pub/sub
internal/engine/engine.go   — Ingest = store.Ingest + bus.Publish
internal/daemon/            — daemon lifecycle + RPC
  paths.go daemon.go rpc.go
internal/client/            — CLI-side RPC client + daemon autostart
  client.go ensure.go
scripts/smoke.sh            — end-to-end smoke test
```

---

### Task 1: Project scaffolding and dependencies

**Files:**
- Modify: `go.mod` (deps)
- Create: directory tree above (empty dirs are created implicitly by later tasks)

- [ ] **Step 1: Add dependencies**

```bash
go get modernc.org/sqlite@latest
go get github.com/spf13/cobra@latest
```

- [ ] **Step 2: Verify module state**

Run: `go mod tidy && go build ./...`
Expected: exits 0 (no packages yet is fine; `matched no packages` warning is OK)

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add sqlite and cobra dependencies"
```

---

### Task 2: Core IDs and Event type

**Files:**
- Create: `internal/core/id.go`, `internal/core/event.go`
- Test: `internal/core/id_test.go`

- [ ] **Step 1: Write the failing test**

```go
package core

import (
	"strings"
	"testing"
)

func TestNewID(t *testing.T) {
	a := NewID("evt")
	b := NewID("evt")
	if a == b {
		t.Fatalf("ids must be unique, got %q twice", a)
	}
	if !strings.HasPrefix(a, "evt-") {
		t.Fatalf("id must start with prefix, got %q", a)
	}
	if len(a) != len("evt-")+12 {
		t.Fatalf("id must have 12 hex chars after prefix, got %q", a)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestNewID -v`
Expected: FAIL (NewID undefined)

- [ ] **Step 3: Implement**

`internal/core/id.go`:

```go
package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewID returns prefix + "-" + 12 random hex chars, e.g. "evt-a1b2c3d4e5f6".
func NewID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return prefix + "-" + hex.EncodeToString(b)
}
```

`internal/core/event.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/
git commit -m "feat(core): event type and id generation"
```

---

### Task 3: EventSelector matching and Rule activity

**Files:**
- Create: `internal/core/rule.go`
- Test: `internal/core/rule_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package core

import (
	"testing"
	"time"
)

func TestEventSelectorMatches(t *testing.T) {
	ev := Event{Type: "docker.healthy", Source: "postgres"}
	cases := []struct {
		name string
		sel  EventSelector
		want bool
	}{
		{"type+source match", EventSelector{Type: "docker.healthy", Source: "postgres"}, true},
		{"empty source matches any", EventSelector{Type: "docker.healthy"}, true},
		{"wrong source", EventSelector{Type: "docker.healthy", Source: "redis"}, false},
		{"wrong type", EventSelector{Type: "http.available", Source: "postgres"}, false},
	}
	for _, c := range cases {
		if got := c.sel.Matches(ev); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestRuleActive(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	cases := []struct {
		name string
		rule Rule
		want bool
	}{
		{"no expiry, not consumed", Rule{}, true},
		{"consumed", Rule{Consumed: true}, false},
		{"expires in future", Rule{ExpiresAt: &future}, true},
		{"expired", Rule{ExpiresAt: &past}, false},
	}
	for _, c := range cases {
		if got := c.rule.Active(now); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/ -run 'TestEventSelector|TestRuleActive' -v`
Expected: FAIL (types undefined)

- [ ] **Step 3: Implement**

`internal/core/rule.go`:

```go
package core

import "time"

// ActionContinueSession is the only action kind in the core plan;
// the field exists because the spec defines action.kind as extensible.
const ActionContinueSession = "continue_session"

// EventSelector matches events by type and (optionally) source.
type EventSelector struct {
	Type   string `json:"type"`
	Source string `json:"source,omitempty"` // empty matches any source
}

func (s EventSelector) Matches(ev Event) bool {
	if s.Type != ev.Type {
		return false
	}
	return s.Source == "" || s.Source == ev.Source
}

// Rule binds an event selector to a continuation action.
type Rule struct {
	ID             string        `json:"id"`
	Selector       EventSelector `json:"eventSelector"`
	ActionKind     string        `json:"actionKind"`
	SessionID      string        `json:"sessionId"`
	PromptTemplate string        `json:"promptTemplate"`
	Label          string        `json:"label,omitempty"`
	OneShot        bool          `json:"oneShot"`
	Consumed       bool          `json:"consumed"`
	ExpiresAt      *time.Time    `json:"expiresAt,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
}

func (r Rule) Active(now time.Time) bool {
	if r.Consumed {
		return false
	}
	return r.ExpiresAt == nil || now.Before(*r.ExpiresAt)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/core/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/rule.go internal/core/rule_test.go
git commit -m "feat(core): rule and event selector with matching/expiry"
```

---

### Task 4: Prompt template rendering

**Files:**
- Modify: `internal/core/rule.go` (add RenderPrompt)
- Test: `internal/core/rule_test.go` (append)

- [ ] **Step 1: Write the failing test (append to rule_test.go)**

```go
func TestRenderPrompt(t *testing.T) {
	ev := Event{
		Type:    "docker.healthy",
		Source:  "postgres",
		Payload: map[string]string{"container": "postgres"},
	}
	r := Rule{PromptTemplate: `{{.Event.Source}} is healthy ({{index .Event.Payload "container"}}). Continue.`}
	got, err := r.RenderPrompt(ev)
	if err != nil {
		t.Fatal(err)
	}
	want := "postgres is healthy (postgres). Continue."
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRenderPromptBadTemplate(t *testing.T) {
	r := Rule{PromptTemplate: `{{.Event.Nope}}`}
	if _, err := r.RenderPrompt(Event{}); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestValidatePromptTemplate(t *testing.T) {
	// Parse-only: payload references are legitimate even though no
	// payload exists at validation time.
	if err := ValidatePromptTemplate(`{{.Event.Payload.container}} ready`); err != nil {
		t.Fatalf("valid template rejected: %v", err)
	}
	if err := ValidatePromptTemplate(`{{.Event.Source`); err == nil {
		t.Fatal("syntax error not caught")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestRenderPrompt -v`
Expected: FAIL (RenderPrompt undefined)

- [ ] **Step 3: Implement (append to rule.go; add imports `strings`, `text/template`)**

```go
// RenderPrompt renders the rule's prompt template with the event as
// {{.Event}}. Only machine-generated event fields enter the context.
func (r Rule) RenderPrompt(ev Event) (string, error) {
	t, err := template.New("prompt").Option("missingkey=error").Parse(r.PromptTemplate)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, struct{ Event Event }{ev}); err != nil {
		return "", err
	}
	return b.String(), nil
}

// ValidatePromptTemplate checks template syntax WITHOUT executing it.
// Execution-time errors (missing payload keys) are legitimate at
// validation time — they surface later as failed continuations.
func ValidatePromptTemplate(s string) error {
	_, err := template.New("prompt").Parse(s)
	return err
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/core/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/
git commit -m "feat(core): prompt template rendering with strict missing-key errors"
```

---

### Task 5: Session and Continuation types

**Files:**
- Create: `internal/core/session.go`, `internal/core/continuation.go`

No behavior — types only, covered by store tests later. No dedicated test file.

- [ ] **Step 1: Implement**

`internal/core/session.go`:

```go
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
```

`internal/core/continuation.go`:

```go
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
	ID            string            `json:"id"`
	RuleID        string            `json:"ruleId"`
	EventID       string            `json:"eventId"`
	SessionID     string            `json:"sessionId"`
	Prompt        string            `json:"prompt"`
	State         ContinuationState `json:"state"`
	Command       string            `json:"command,omitempty"`
	ExitCode      *int              `json:"exitCode,omitempty"`
	OutputSummary string            `json:"outputSummary,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./... && go test ./internal/core/`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/core/
git commit -m "feat(core): session and continuation types"
```

---

### Task 6: Store open + schema migration

**Files:**
- Create: `internal/store/store.go`
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenMigrates(t *testing.T) {
	s := openTestStore(t)
	for _, table := range []string{"events", "rules", "sessions", "continuations"} {
		var n int
		err := s.db.QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&n)
		if err != nil || n != 1 {
			t.Fatalf("table %s missing (n=%d err=%v)", table, n, err)
		}
	}
	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q err=%v, want wal", mode, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -v`
Expected: FAIL (package doesn't compile / Open undefined)

- [ ] **Step 3: Implement**

`internal/store/store.go`:

```go
// Package store persists RuntimePulse state in a single SQLite database.
package store

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite",
		"file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// modernc/sqlite allows one writer; a single connection keeps the
	// outbox transaction serialization trivial.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS events (
  id         TEXT PRIMARY KEY,
  type       TEXT NOT NULL,
  source     TEXT NOT NULL,
  payload    TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_type_source ON events(type, source);
CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at);

CREATE TABLE IF NOT EXISTS rules (
  id              TEXT PRIMARY KEY,
  event_type      TEXT NOT NULL,
  event_source    TEXT NOT NULL DEFAULT '',
  action_kind     TEXT NOT NULL,
  session_id      TEXT NOT NULL,
  prompt_template TEXT NOT NULL,
  label           TEXT NOT NULL DEFAULT '',
  one_shot        INTEGER NOT NULL DEFAULT 0,
  consumed        INTEGER NOT NULL DEFAULT 0,
  expires_at      TEXT,
  created_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rules_match ON rules(event_type, consumed);

CREATE TABLE IF NOT EXISTS sessions (
  session_id TEXT PRIMARY KEY,
  agent      TEXT NOT NULL,
  repo_path  TEXT NOT NULL,
  state      TEXT NOT NULL DEFAULT 'waiting',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS continuations (
  id             TEXT PRIMARY KEY,
  rule_id        TEXT NOT NULL,
  event_id       TEXT NOT NULL,
  session_id     TEXT NOT NULL,
  prompt         TEXT NOT NULL,
  state          TEXT NOT NULL DEFAULT 'pending',
  command        TEXT NOT NULL DEFAULT '',
  exit_code      INTEGER,
  output_summary TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  UNIQUE(rule_id, event_id)
);
CREATE INDEX IF NOT EXISTS idx_continuations_state ON continuations(state);
`

func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

// ts/parseTS: all times stored as RFC3339Nano UTC strings.
func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTS(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): sqlite open with WAL and schema migration"
```

---

### Task 7: Event persistence (insert, list, prune)

**Files:**
- Create: `internal/store/events.go`
- Test: `internal/store/events_test.go`

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func testEvent(id string, at time.Time) core.Event {
	return core.Event{
		ID: id, Type: "docker.healthy", Source: "postgres",
		Payload:   map[string]string{"container": "postgres"},
		Timestamp: at,
	}
}

func TestInsertAndListEvents(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	if err := s.InsertEvent(testEvent("evt-1", now)); err != nil {
		t.Fatal(err)
	}
	// duplicate id is ignored, not an error (idempotent re-ingest)
	if err := s.InsertEvent(testEvent("evt-1", now)); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertEvent(testEvent("evt-2", now.Add(time.Second))); err != nil {
		t.Fatal(err)
	}

	evs, err := s.ListEvents("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2", len(evs))
	}
	if evs[0].ID != "evt-2" {
		t.Fatalf("newest first: got %s", evs[0].ID)
	}
	if evs[0].Payload["container"] != "postgres" {
		t.Fatalf("payload lost: %#v", evs[0].Payload)
	}

	byType, err := s.ListEvents("http.available", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(byType) != 0 {
		t.Fatalf("type filter failed, got %d", len(byType))
	}
}

func TestPruneEvents(t *testing.T) {
	s := openTestStore(t)
	old := time.Now().UTC().Add(-48 * time.Hour)
	s.InsertEvent(testEvent("evt-old", old))
	s.InsertEvent(testEvent("evt-new", time.Now().UTC()))
	n, err := s.PruneEvents(time.Now().UTC().Add(-24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned %d, want 1", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestInsertAndList|TestPrune' -v`
Expected: FAIL (methods undefined)

- [ ] **Step 3: Implement**

`internal/store/events.go`:

```go
package store

import (
	"database/sql"
	"encoding/json"

	"github.com/tufantunc/RuntimePulse/internal/core"
	"time"
)

// InsertEvent stores an event; re-inserting the same id is a no-op.
func (s *Store) InsertEvent(ev core.Event) error {
	return insertEventTx(s.db, ev)
}

// execer lets the same statements run on *sql.DB and *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func insertEventTx(e execer, ev core.Event) error {
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		return err
	}
	_, err = e.Exec(
		`INSERT OR IGNORE INTO events (id, type, source, payload, created_at) VALUES (?,?,?,?,?)`,
		ev.ID, ev.Type, ev.Source, string(payload), ts(ev.Timestamp))
	return err
}

// ListEvents returns newest-first events, optionally filtered by type.
func (s *Store) ListEvents(evType string, limit int) ([]core.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id, type, source, payload, created_at FROM events `
	args := []any{}
	if evType != "" {
		q += `WHERE type = ? `
		args = append(args, evType)
	}
	q += `ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []core.Event
	for rows.Next() {
		var ev core.Event
		var payload, created string
		if err := rows.Scan(&ev.ID, &ev.Type, &ev.Source, &payload, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &ev.Payload); err != nil {
			return nil, err
		}
		ev.Timestamp = parseTS(created)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// PruneEvents deletes events older than the cutoff, returning the count.
func (s *Store) PruneEvents(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM events WHERE created_at < ?`, ts(before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/store/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): event insert/list/prune with idempotent inserts"
```

---

### Task 8: Session persistence

**Files:**
- Create: `internal/store/sessions.go`
- Test: `internal/store/sessions_test.go`

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestRegisterAndListSessions(t *testing.T) {
	s := openTestStore(t)
	sess, err := s.RegisterSession(core.Session{
		SessionID: "abc123", Agent: "claude", RepoPath: "/tmp/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sess.State != core.SessionWaiting {
		t.Fatalf("default state = %s, want waiting", sess.State)
	}
	if sess.CreatedAt.IsZero() {
		t.Fatal("CreatedAt not set")
	}

	// re-register upserts (agent change sticks, no duplicate)
	if _, err := s.RegisterSession(core.Session{
		SessionID: "abc123", Agent: "cursor", RepoPath: "/tmp/repo2",
	}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Agent != "cursor" || list[0].RepoPath != "/tmp/repo2" {
		t.Fatalf("upsert failed: %#v", list)
	}

	got, ok, err := s.GetSession("abc123")
	if err != nil || !ok || got.SessionID != "abc123" {
		t.Fatalf("GetSession: %#v ok=%v err=%v", got, ok, err)
	}
	if _, ok, _ := s.GetSession("missing"); ok {
		t.Fatal("GetSession should report missing")
	}

	if err := s.SetSessionState("abc123", core.SessionQueued); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.GetSession("abc123")
	if got.State != core.SessionQueued {
		t.Fatalf("state = %s, want queued", got.State)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestRegisterAndList -v`
Expected: FAIL (methods undefined)

- [ ] **Step 3: Implement**

`internal/store/sessions.go`:

```go
package store

import (
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// RegisterSession upserts a session; new sessions start as waiting.
func (s *Store) RegisterSession(sess core.Session) (core.Session, error) {
	now := time.Now().UTC()
	if sess.State == "" {
		sess.State = core.SessionWaiting
	}
	sess.CreatedAt, sess.UpdatedAt = now, now
	_, err := s.db.Exec(`
		INSERT INTO sessions (session_id, agent, repo_path, state, created_at, updated_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET
		  agent = excluded.agent, repo_path = excluded.repo_path, updated_at = excluded.updated_at`,
		sess.SessionID, sess.Agent, sess.RepoPath, string(sess.State), ts(now), ts(now))
	return sess, err
}

func (s *Store) GetSession(id string) (core.Session, bool, error) {
	row := s.db.QueryRow(
		`SELECT session_id, agent, repo_path, state, created_at, updated_at
		 FROM sessions WHERE session_id = ?`, id)
	sess, err := scanSession(row)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return core.Session{}, false, nil
		}
		return core.Session{}, false, err
	}
	return sess, true, nil
}

func (s *Store) ListSessions() ([]core.Session, error) {
	rows, err := s.db.Query(
		`SELECT session_id, agent, repo_path, state, created_at, updated_at
		 FROM sessions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *Store) SetSessionState(id string, st core.SessionState) error {
	_, err := s.db.Exec(`UPDATE sessions SET state = ?, updated_at = ? WHERE session_id = ?`,
		string(st), ts(time.Now().UTC()), id)
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSession(r rowScanner) (core.Session, error) {
	var sess core.Session
	var state, created, updated string
	if err := r.Scan(&sess.SessionID, &sess.Agent, &sess.RepoPath, &state, &created, &updated); err != nil {
		return core.Session{}, err
	}
	sess.State = core.SessionState(state)
	sess.CreatedAt, sess.UpdatedAt = parseTS(created), parseTS(updated)
	return sess, nil
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/store/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): session registry with upsert and state transitions"
```

---

### Task 9: Rule persistence

**Files:**
- Create: `internal/store/rules.go`
- Test: `internal/store/rules_test.go`

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func testRule(sel core.EventSelector) core.Rule {
	return core.Rule{
		Selector:       sel,
		ActionKind:     core.ActionContinueSession,
		SessionID:      "abc123",
		PromptTemplate: "go on",
	}
}

func TestAddListRemoveRules(t *testing.T) {
	s := openTestStore(t)
	r, err := s.AddRule(testRule(core.EventSelector{Type: "docker.healthy", Source: "postgres"}))
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == "" || r.CreatedAt.IsZero() {
		t.Fatalf("AddRule must fill id and created_at: %#v", r)
	}

	past := time.Now().UTC().Add(-time.Hour)
	expired := testRule(core.EventSelector{Type: "docker.healthy"})
	expired.ExpiresAt = &past
	if _, err := s.AddRule(expired); err != nil {
		t.Fatal(err)
	}

	active, err := s.ListRules(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != r.ID {
		t.Fatalf("ListRules(false) must exclude expired: %#v", active)
	}
	all, err := s.ListRules(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ListRules(true) = %d, want 2", len(all))
	}

	if err := s.RemoveRule(r.ID); err != nil {
		t.Fatal(err)
	}
	active, _ = s.ListRules(false)
	if len(active) != 0 {
		t.Fatalf("rule not removed: %#v", active)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestAddListRemove -v`
Expected: FAIL

- [ ] **Step 3: Implement**

`internal/store/rules.go`:

```go
package store

import (
	"database/sql"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// AddRule stores a rule, assigning id and created_at.
func (s *Store) AddRule(r core.Rule) (core.Rule, error) {
	if r.ID == "" {
		r.ID = core.NewID("rule")
	}
	if r.ActionKind == "" {
		r.ActionKind = core.ActionContinueSession
	}
	r.CreatedAt = time.Now().UTC()
	var expires any
	if r.ExpiresAt != nil {
		expires = ts(*r.ExpiresAt)
	}
	_, err := s.db.Exec(`
		INSERT INTO rules (id, event_type, event_source, action_kind, session_id,
		                   prompt_template, label, one_shot, consumed, expires_at, created_at)
		VALUES (?,?,?,?,?,?,?,?,0,?,?)`,
		r.ID, r.Selector.Type, r.Selector.Source, r.ActionKind, r.SessionID,
		r.PromptTemplate, r.Label, boolInt(r.OneShot), expires, ts(r.CreatedAt))
	return r, err
}

// ListRules returns rules; includeInactive=false filters out consumed and expired.
func (s *Store) ListRules(includeInactive bool) ([]core.Rule, error) {
	q := `SELECT id, event_type, event_source, action_kind, session_id,
	             prompt_template, label, one_shot, consumed, expires_at, created_at
	      FROM rules`
	args := []any{}
	if !includeInactive {
		q += ` WHERE consumed = 0 AND (expires_at IS NULL OR expires_at > ?)`
		args = append(args, ts(time.Now().UTC()))
	}
	q += ` ORDER BY created_at`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RemoveRule(id string) error {
	_, err := s.db.Exec(`DELETE FROM rules WHERE id = ?`, id)
	return err
}

func scanRule(r rowScanner) (core.Rule, error) {
	var rule core.Rule
	var oneShot, consumed int
	var expires sql.NullString
	var created string
	if err := r.Scan(&rule.ID, &rule.Selector.Type, &rule.Selector.Source, &rule.ActionKind,
		&rule.SessionID, &rule.PromptTemplate, &rule.Label, &oneShot, &consumed,
		&expires, &created); err != nil {
		return core.Rule{}, err
	}
	rule.OneShot, rule.Consumed = oneShot == 1, consumed == 1
	if expires.Valid {
		t := parseTS(expires.String)
		rule.ExpiresAt = &t
	}
	rule.CreatedAt = parseTS(created)
	return rule, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/store/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): rule persistence with active filtering"
```

---

### Task 10: Transactional ingest (outbox) — the heart of the core

**Files:**
- Create: `internal/store/ingest.go`
- Test: `internal/store/ingest_test.go`

Behavior (spec §5): in ONE transaction — insert event, match active rules, render prompts, create pending continuations (`INSERT OR IGNORE` on `(rule_id,event_id)`), consume oneShot rules. A template render error produces a `failed` continuation carrying the error (surfaces bad templates instead of silently dropping the wake-up).

- [ ] **Step 1: Write the failing tests**

```go
package store

import (
	"strings"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func mustAddRule(t *testing.T, s *Store, r core.Rule) core.Rule {
	t.Helper()
	added, err := s.AddRule(r)
	if err != nil {
		t.Fatal(err)
	}
	return added
}

func ingestEvent(t *testing.T, s *Store, id string) IngestResult {
	t.Helper()
	res, err := s.Ingest(core.Event{
		ID: id, Type: "docker.healthy", Source: "postgres",
		Payload: map[string]string{"container": "postgres"}, Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestIngestMatchesAndCreatesContinuation(t *testing.T) {
	s := openTestStore(t)
	r := mustAddRule(t, s, core.Rule{
		Selector:       core.EventSelector{Type: "docker.healthy", Source: "postgres"},
		SessionID:      "abc123",
		PromptTemplate: "{{.Event.Source}} healthy. Continue.",
		OneShot:        true,
	})
	// non-matching rule must not fire
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "http.available"}, SessionID: "abc123", PromptTemplate: "x",
	})

	res := ingestEvent(t, s, "evt-1")
	if len(res.Continuations) != 1 {
		t.Fatalf("got %d continuations, want 1", len(res.Continuations))
	}
	c := res.Continuations[0]
	if c.Prompt != "postgres healthy. Continue." {
		t.Fatalf("prompt = %q", c.Prompt)
	}
	if c.State != core.ContinuationPending || c.RuleID != r.ID || c.EventID != "evt-1" {
		t.Fatalf("bad continuation: %#v", c)
	}

	// oneShot rule is consumed in the same transaction
	active, _ := s.ListRules(false)
	for _, a := range active {
		if a.ID == r.ID {
			t.Fatal("oneShot rule should be consumed")
		}
	}
}

func TestIngestIsIdempotent(t *testing.T) {
	s := openTestStore(t)
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "docker.healthy"}, SessionID: "abc123", PromptTemplate: "go",
	})
	first := ingestEvent(t, s, "evt-1")
	second := ingestEvent(t, s, "evt-1") // same event re-delivered
	if len(first.Continuations) != 1 || len(second.Continuations) != 0 {
		t.Fatalf("idempotency broken: first=%d second=%d",
			len(first.Continuations), len(second.Continuations))
	}
	all, err := s.ListContinuations("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("store has %d continuations, want 1", len(all))
	}
}

func TestIngestSkipsExpiredRules(t *testing.T) {
	s := openTestStore(t)
	past := time.Now().UTC().Add(-time.Minute)
	r := core.Rule{
		Selector: core.EventSelector{Type: "docker.healthy"}, SessionID: "abc123", PromptTemplate: "go",
	}
	r.ExpiresAt = &past
	mustAddRule(t, s, r)
	if res := ingestEvent(t, s, "evt-1"); len(res.Continuations) != 0 {
		t.Fatal("expired rule must not fire")
	}
}

func TestIngestRenderErrorYieldsFailedContinuation(t *testing.T) {
	s := openTestStore(t)
	mustAddRule(t, s, core.Rule{
		Selector: core.EventSelector{Type: "docker.healthy"}, SessionID: "abc123",
		PromptTemplate: "{{.Event.Bogus}}",
	})
	res := ingestEvent(t, s, "evt-1")
	if len(res.Continuations) != 1 {
		t.Fatalf("got %d, want 1 failed continuation", len(res.Continuations))
	}
	c := res.Continuations[0]
	if c.State != core.ContinuationFailed || !strings.Contains(c.OutputSummary, "Bogus") {
		t.Fatalf("render error not surfaced: %#v", c)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run TestIngest -v`
Expected: FAIL (Ingest undefined)

- [ ] **Step 3: Implement**

`internal/store/ingest.go`:

```go
package store

import (
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// IngestResult reports what one event produced.
type IngestResult struct {
	Event         core.Event          `json:"event"`
	Continuations []core.Continuation `json:"continuations"`
}

// Ingest is the outbox transaction (spec §5): store the event, match
// active rules, render prompts, create pending continuations, and
// consume oneShot rules — atomically. (rule_id, event_id) uniqueness
// makes re-delivery of the same event a no-op.
func (s *Store) Ingest(ev core.Event) (IngestResult, error) {
	res := IngestResult{Event: ev}
	tx, err := s.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	if err := insertEventTx(tx, ev); err != nil {
		return res, err
	}

	now := time.Now().UTC()
	rows, err := tx.Query(`
		SELECT id, event_type, event_source, action_kind, session_id,
		       prompt_template, label, one_shot, consumed, expires_at, created_at
		FROM rules
		WHERE event_type = ? AND (event_source = '' OR event_source = ?)
		  AND consumed = 0 AND (expires_at IS NULL OR expires_at > ?)`,
		ev.Type, ev.Source, ts(now))
	if err != nil {
		return res, err
	}
	var matched []core.Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			rows.Close()
			return res, err
		}
		matched = append(matched, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}

	for _, r := range matched {
		c := core.Continuation{
			ID: core.NewID("cont"), RuleID: r.ID, EventID: ev.ID, SessionID: r.SessionID,
			State: core.ContinuationPending, CreatedAt: now, UpdatedAt: now,
		}
		prompt, err := r.RenderPrompt(ev)
		if err != nil {
			c.State = core.ContinuationFailed
			c.OutputSummary = "prompt template error: " + err.Error()
		} else {
			c.Prompt = prompt
		}
		ins, err := tx.Exec(`
			INSERT OR IGNORE INTO continuations
			  (id, rule_id, event_id, session_id, prompt, state, output_summary, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			c.ID, c.RuleID, c.EventID, c.SessionID, c.Prompt, string(c.State),
			c.OutputSummary, ts(now), ts(now))
		if err != nil {
			return res, err
		}
		n, err := ins.RowsAffected()
		if err != nil {
			return res, err
		}
		if n == 0 {
			continue // this (rule, event) pair already produced a continuation
		}
		if r.OneShot {
			if _, err := tx.Exec(`UPDATE rules SET consumed = 1 WHERE id = ?`, r.ID); err != nil {
				return res, err
			}
		}
		res.Continuations = append(res.Continuations, c)
	}

	if err := tx.Commit(); err != nil {
		return res, err
	}
	return res, nil
}

// ListContinuations returns continuations, optionally filtered by state.
func (s *Store) ListContinuations(state string) ([]core.Continuation, error) {
	q := `SELECT id, rule_id, event_id, session_id, prompt, state, command,
	             exit_code, output_summary, created_at, updated_at
	      FROM continuations`
	args := []any{}
	if state != "" {
		q += ` WHERE state = ?`
		args = append(args, state)
	}
	q += ` ORDER BY created_at`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Continuation
	for rows.Next() {
		var c core.Continuation
		var st, created, updated string
		var exit *int
		if err := rows.Scan(&c.ID, &c.RuleID, &c.EventID, &c.SessionID, &c.Prompt,
			&st, &c.Command, &exit, &c.OutputSummary, &created, &updated); err != nil {
			return nil, err
		}
		c.State = core.ContinuationState(st)
		c.ExitCode = exit
		c.CreatedAt, c.UpdatedAt = parseTS(created), parseTS(updated)
		out = append(out, c)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run all store tests, verify pass**

Run: `go test ./internal/store/ -v`
Expected: PASS (all)

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): transactional outbox ingest with idempotency and oneShot consumption"
```

---

### Task 11: Event bus

**Files:**
- Create: `internal/bus/bus.go`
- Test: `internal/bus/bus_test.go`

- [ ] **Step 1: Write the failing test**

```go
package bus

import (
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestPublishSubscribe(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(4)
	defer cancel()
	b.Publish(core.Event{ID: "evt-1"})
	select {
	case ev := <-ch:
		if ev.ID != "evt-1" {
			t.Fatalf("got %q", ev.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("event not delivered")
	}
}

func TestCancelClosesChannel(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(1)
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel must be closed after cancel")
	}
	b.Publish(core.Event{ID: "evt-2"}) // must not panic
}

func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	b := New()
	_, cancel := b.Subscribe(1) // never read
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			b.Publish(core.Event{ID: "evt"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked on a full subscriber")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bus/ -v`
Expected: FAIL

- [ ] **Step 3: Implement**

`internal/bus/bus.go`:

```go
// Package bus is the in-process event bus: live notification only.
// Durability lives in the store; a dropped bus message is never data loss.
package bus

import (
	"sync"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

type Bus struct {
	mu   sync.Mutex
	subs map[int]chan core.Event
	next int
}

func New() *Bus {
	return &Bus{subs: map[int]chan core.Event{}}
}

// Subscribe returns a buffered channel and a cancel func. Cancel closes
// the channel and unregisters it; calling cancel twice is safe.
func (b *Bus) Subscribe(buffer int) (<-chan core.Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan core.Event, buffer)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}
}

// Publish never blocks: a full subscriber's message is dropped.
func (b *Bus) Publish(ev core.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/bus/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bus/
git commit -m "feat(bus): non-blocking in-process pub/sub"
```

---

### Task 12: Engine (ingest pipeline)

**Files:**
- Create: `internal/engine/engine.go`
- Test: `internal/engine/engine_test.go`

- [ ] **Step 1: Write the failing test**

```go
package engine

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/bus"
	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/store"
)

func TestIngestFillsFieldsAndPublishes(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := bus.New()
	e := New(st, b)

	ch, cancel := b.Subscribe(4)
	defer cancel()

	res, err := e.Ingest(core.Event{Type: "http.available", Source: "localhost:3000"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Event.ID == "" || res.Event.Timestamp.IsZero() {
		t.Fatalf("engine must fill id/timestamp: %#v", res.Event)
	}
	select {
	case got := <-ch:
		if got.ID != res.Event.ID {
			t.Fatalf("published %q, ingested %q", got.ID, res.Event.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("event not published to bus")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -v`
Expected: FAIL

- [ ] **Step 3: Implement**

`internal/engine/engine.go`:

```go
// Package engine wires the store's transactional ingest to the live bus.
package engine

import (
	"time"

	"github.com/tufantunc/RuntimePulse/internal/bus"
	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/store"
)

type Engine struct {
	Store *store.Store
	Bus   *bus.Bus
}

func New(st *store.Store, b *bus.Bus) *Engine {
	return &Engine{Store: st, Bus: b}
}

// Ingest normalizes the event, runs the outbox transaction, and only
// then notifies live subscribers (durable first, notify second).
func (e *Engine) Ingest(ev core.Event) (store.IngestResult, error) {
	if ev.ID == "" {
		ev.ID = core.NewID("evt")
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	res, err := e.Store.Ingest(ev)
	if err != nil {
		return res, err
	}
	e.Bus.Publish(res.Event)
	return res, nil
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/engine/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/
git commit -m "feat(engine): ingest pipeline — durable outbox then bus publish"
```

---

### Task 13: Daemon — state dir, socket, RPC protocol, handlers

**Files:**
- Create: `internal/daemon/paths.go`, `internal/daemon/daemon.go`, `internal/daemon/rpc.go`, `internal/client/client.go`
- Test: `internal/daemon/daemon_test.go`

Protocol: NDJSON over the unix socket. Request `{"id":1,"method":"...","params":{...}}` → response `{"id":1,"result":...}` or `{"id":1,"error":"..."}`. One connection may carry many requests. (`events.follow` streaming is Task 14.)

- [ ] **Step 1: Write the failing test**

```go
package daemon

import (
	"context"
	"os"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/client"
)

// Short tempdir: macOS unix socket paths are limited to ~104 bytes.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func startTestDaemon(t *testing.T) (*Daemon, *client.Client) {
	t.Helper()
	dir := shortTempDir(t)
	d, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); d.Close() })
	go d.Serve(ctx)
	return d, client.New(SocketPath(dir))
}

func TestStatusRPC(t *testing.T) {
	_, c := startTestDaemon(t)
	var st struct {
		Version string `json:"version"`
		Rules   int    `json:"rules"`
	}
	if err := c.Call("status", nil, &st); err != nil {
		t.Fatal(err)
	}
	if st.Version == "" {
		t.Fatal("status must report version")
	}
}

func TestRuleSessionInjectFlow(t *testing.T) {
	_, c := startTestDaemon(t)

	var sess map[string]any
	err := c.Call("session.register",
		map[string]any{"sessionId": "abc123", "agent": "claude", "repoPath": "/tmp/x"}, &sess)
	if err != nil {
		t.Fatal(err)
	}

	var rule map[string]any
	err = c.Call("rule.add", map[string]any{
		"type": "docker.healthy", "source": "postgres",
		"sessionId": "abc123", "prompt": "{{.Event.Source}} ready. Continue.", "oneShot": true,
	}, &rule)
	if err != nil {
		t.Fatal(err)
	}

	// rule.add against an unknown session must fail
	err = c.Call("rule.add", map[string]any{
		"type": "x", "sessionId": "missing", "prompt": "p",
	}, &map[string]any{})
	if err == nil {
		t.Fatal("rule.add for unregistered session must error")
	}

	var res struct {
		Continuations []map[string]any `json:"continuations"`
	}
	err = c.Call("event.inject",
		map[string]any{"type": "docker.healthy", "source": "postgres"}, &res)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Continuations) != 1 {
		t.Fatalf("inject produced %d continuations, want 1", len(res.Continuations))
	}
	if res.Continuations[0]["prompt"] != "postgres ready. Continue." {
		t.Fatalf("prompt = %v", res.Continuations[0]["prompt"])
	}

	var evs []map[string]any
	if err := c.Call("events.list", map[string]any{"limit": 10}, &evs); err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("events.list = %d, want 1", len(evs))
	}
}

func TestSocketPermissions(t *testing.T) {
	d, _ := startTestDaemon(t)
	info, err := os.Stat(SocketPath(d.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket perm = %o, want 0600", perm)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -v`
Expected: FAIL (packages don't exist)

- [ ] **Step 3: Implement paths**

`internal/daemon/paths.go`:

```go
package daemon

import (
	"os"
	"path/filepath"
)

// StateDir resolves the daemon's state directory:
// $RUNTIMEPULSE_DIR if set, else ~/.runtimepulse.
func StateDir() (string, error) {
	if d := os.Getenv("RUNTIMEPULSE_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".runtimepulse"), nil
}

func SocketPath(dir string) string { return filepath.Join(dir, "daemon.sock") }
func DBPath(dir string) string     { return filepath.Join(dir, "runtimepulse.db") }
func LogPath(dir string) string    { return filepath.Join(dir, "daemon.log") }
```

- [ ] **Step 4: Implement daemon lifecycle**

`internal/daemon/daemon.go`:

```go
// Package daemon hosts the engine behind a unix-socket RPC server.
package daemon

import (
	"context"
	"net"
	"os"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/bus"
	"github.com/tufantunc/RuntimePulse/internal/engine"
	"github.com/tufantunc/RuntimePulse/internal/store"
)

const Version = "0.1.0-dev"

type Daemon struct {
	Dir    string
	Engine *engine.Engine

	store *store.Store
	bus   *bus.Bus
	ln    net.Listener
}

func New(dir string) (*Daemon, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	st, err := store.Open(DBPath(dir))
	if err != nil {
		return nil, err
	}
	b := bus.New()

	sock := SocketPath(dir)
	removeStaleSocket(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		st.Close()
		return nil, err // includes "address already in use" → daemon already running
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		ln.Close()
		st.Close()
		return nil, err
	}
	return &Daemon{Dir: dir, Engine: engine.New(st, b), store: st, bus: b, ln: ln}, nil
}

// removeStaleSocket deletes a socket file nobody is listening on.
func removeStaleSocket(path string) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		os.Remove(path)
		return
	}
	conn.Close() // live daemon; Listen will fail loudly
}

// Serve accepts connections until ctx is cancelled.
func (d *Daemon) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		d.ln.Close()
	}()
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go d.handleConn(conn)
	}
}

func (d *Daemon) Close() {
	d.ln.Close()
	d.store.Close()
	os.Remove(SocketPath(d.Dir))
}
```

- [ ] **Step 5: Implement RPC**

`internal/daemon/rpc.go`:

```go
package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

type request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type response struct {
	ID     int64  `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (d *Daemon) handleConn(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			return // client closed or sent garbage; drop the connection
		}
		if req.Method == "events.follow" {
			d.follow(conn, enc, req) // Task 14; streams until disconnect
			return
		}
		result, err := d.dispatch(req.Method, req.Params)
		resp := response{ID: req.ID, Result: result}
		if err != nil {
			resp = response{ID: req.ID, Error: err.Error()}
		}
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

func unmarshalParams[T any](raw json.RawMessage) (T, error) {
	var p T
	if len(raw) == 0 {
		return p, nil
	}
	err := json.Unmarshal(raw, &p)
	return p, err
}

func (d *Daemon) dispatch(method string, params json.RawMessage) (any, error) {
	switch method {
	case "status":
		return d.status()
	case "event.inject":
		p, err := unmarshalParams[struct {
			Type    string            `json:"type"`
			Source  string            `json:"source"`
			Payload map[string]string `json:"payload"`
		}](params)
		if err != nil {
			return nil, err
		}
		if p.Type == "" {
			return nil, errors.New("event.inject: type is required")
		}
		return d.Engine.Ingest(core.Event{Type: p.Type, Source: p.Source, Payload: p.Payload})
	case "events.list":
		p, err := unmarshalParams[struct {
			Type  string `json:"type"`
			Limit int    `json:"limit"`
		}](params)
		if err != nil {
			return nil, err
		}
		return d.Engine.Store.ListEvents(p.Type, p.Limit)
	case "rule.add":
		return d.ruleAdd(params)
	case "rule.list":
		p, err := unmarshalParams[struct {
			All bool `json:"all"`
		}](params)
		if err != nil {
			return nil, err
		}
		return d.Engine.Store.ListRules(p.All)
	case "rule.remove":
		p, err := unmarshalParams[struct {
			ID string `json:"id"`
		}](params)
		if err != nil {
			return nil, err
		}
		return "ok", d.Engine.Store.RemoveRule(p.ID)
	case "session.register":
		p, err := unmarshalParams[core.Session](params)
		if err != nil {
			return nil, err
		}
		if p.SessionID == "" || p.Agent == "" {
			return nil, errors.New("session.register: sessionId and agent are required")
		}
		return d.Engine.Store.RegisterSession(p)
	case "session.list":
		return d.Engine.Store.ListSessions()
	case "continuation.list":
		p, err := unmarshalParams[struct {
			State string `json:"state"`
		}](params)
		if err != nil {
			return nil, err
		}
		return d.Engine.Store.ListContinuations(p.State)
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}

func (d *Daemon) status() (any, error) {
	events, err := d.Engine.Store.ListEvents("", 1)
	if err != nil {
		return nil, err
	}
	rules, err := d.Engine.Store.ListRules(false)
	if err != nil {
		return nil, err
	}
	sessions, err := d.Engine.Store.ListSessions()
	if err != nil {
		return nil, err
	}
	pending, err := d.Engine.Store.ListContinuations("pending")
	if err != nil {
		return nil, err
	}
	lastEvent := ""
	if len(events) > 0 {
		lastEvent = events[0].Type + " @ " + events[0].Timestamp.Format(time.RFC3339)
	}
	return map[string]any{
		"version":              Version,
		"rules":                len(rules),
		"sessions":             len(sessions),
		"pendingContinuations": len(pending),
		"lastEvent":            lastEvent,
	}, nil
}

func (d *Daemon) ruleAdd(params json.RawMessage) (any, error) {
	p, err := unmarshalParams[struct {
		Type      string `json:"type"`
		Source    string `json:"source"`
		SessionID string `json:"sessionId"`
		Agent     string `json:"agent"`
		RepoPath  string `json:"repoPath"`
		Prompt    string `json:"prompt"`
		Label     string `json:"label"`
		OneShot   bool   `json:"oneShot"`
		ExpiresAt string `json:"expiresAt"` // RFC3339, optional
	}](params)
	if err != nil {
		return nil, err
	}
	if p.Type == "" || p.SessionID == "" || p.Prompt == "" {
		return nil, errors.New("rule.add: type, sessionId and prompt are required")
	}

	// Self-registration path (spec §4.4): agent+repoPath registers the
	// session in the same call. Otherwise the session must already exist.
	if p.Agent != "" && p.RepoPath != "" {
		if _, err := d.Engine.Store.RegisterSession(core.Session{
			SessionID: p.SessionID, Agent: p.Agent, RepoPath: p.RepoPath,
		}); err != nil {
			return nil, err
		}
	} else if _, ok, err := d.Engine.Store.GetSession(p.SessionID); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("rule.add: unknown session %q (register it or pass agent+repoPath)", p.SessionID)
	}

	r := core.Rule{
		Selector:       core.EventSelector{Type: p.Type, Source: p.Source},
		ActionKind:     core.ActionContinueSession,
		SessionID:      p.SessionID,
		PromptTemplate: p.Prompt,
		Label:          p.Label,
		OneShot:        p.OneShot,
	}
	if p.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, p.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("rule.add: bad expiresAt: %w", err)
		}
		r.ExpiresAt = &t
	}
	// Syntax-check the template now so a malformed rule fails at
	// creation; execution errors surface later as failed continuations.
	if err := core.ValidatePromptTemplate(p.Prompt); err != nil {
		return nil, fmt.Errorf("rule.add: bad prompt template: %w", err)
	}
	return d.Engine.Store.AddRule(r)
}
```

> Note: `follow` is referenced but implemented in Task 14. To keep this task compiling, add a stub at the bottom of `rpc.go` — Task 14 replaces it:
>
> ```go
> func (d *Daemon) follow(conn net.Conn, enc *json.Encoder, req request) {
> 	enc.Encode(response{ID: req.ID, Error: "events.follow: not implemented yet"})
> }
> ```

- [ ] **Step 6: Implement the client**

`internal/client/client.go`:

```go
// Package client is the CLI-side RPC client for the daemon socket.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

type Client struct {
	Socket string
}

func New(socket string) *Client { return &Client{Socket: socket} }

type request struct {
	ID     int64 `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Call dials, sends one request, decodes one response into out (out may be nil).
func (c *Client) Call(method string, params, out any) error {
	conn, err := net.DialTimeout("unix", c.Socket, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(request{ID: 1, Method: method, Params: params}); err != nil {
		return err
	}
	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// Follow streams events to fn until ctx is cancelled or the daemon closes.
// (Server side lands in the next task.)
func (c *Client) Follow(ctx context.Context, fn func(core.Event)) error {
	conn, err := net.DialTimeout("unix", c.Socket, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	if err := json.NewEncoder(conn).Encode(request{ID: 1, Method: "events.follow"}); err != nil {
		return err
	}
	dec := json.NewDecoder(conn)
	var first response
	if err := dec.Decode(&first); err != nil {
		return err
	}
	if first.Error != "" {
		return errors.New(first.Error)
	}
	for {
		var note struct {
			Method string     `json:"method"`
			Params core.Event `json:"params"`
		}
		if err := dec.Decode(&note); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if note.Method == "event" {
			fn(note.Params)
		}
	}
}
```

- [ ] **Step 7: Run tests, verify pass**

Run: `go test ./internal/daemon/ -v && go build ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/daemon/ internal/client/
git commit -m "feat(daemon): unix-socket NDJSON RPC with rule/session/event handlers"
```

---

### Task 14: events.follow streaming

**Files:**
- Modify: `internal/daemon/rpc.go` (replace the `follow` stub)
- Test: `internal/daemon/follow_test.go`

- [ ] **Step 1: Write the failing test**

```go
package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestEventsFollow(t *testing.T) {
	d, c := startTestDaemon(t)

	got := make(chan core.Event, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		_ = c.Follow(ctx, func(ev core.Event) { got <- ev })
	}()

	// Give the follower a beat to subscribe, then inject.
	time.Sleep(100 * time.Millisecond)
	if _, err := d.Engine.Ingest(core.Event{Type: "file.changed", Source: "dist/index.js"}); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-got:
		if ev.Type != "file.changed" {
			t.Fatalf("got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follow did not deliver the event")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestEventsFollow -v`
Expected: FAIL (stub returns "not implemented")

- [ ] **Step 3: Replace the stub in rpc.go**

```go
type notification struct {
	Method string     `json:"method"`
	Params core.Event `json:"params"`
}

// follow streams bus events as notifications until the client disconnects.
func (d *Daemon) follow(conn net.Conn, enc *json.Encoder, req request) {
	ch, cancel := d.bus.Subscribe(64)
	defer cancel()
	if err := enc.Encode(response{ID: req.ID, Result: "ok"}); err != nil {
		return
	}
	// Detect client disconnect: the client never sends more data on a
	// follow connection, so a read returning is a hang-up.
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		for {
			if _, err := conn.Read(buf); err != nil {
				close(done)
				return
			}
		}
	}()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := enc.Encode(notification{Method: "event", Params: ev}); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}
```

> Note: `handleConn` uses a `json.Decoder` which may buffer; for the follow path we pass the raw `conn` — the client sends nothing after the follow request, so decoder buffering is not an issue in practice.

- [ ] **Step 4: Run all daemon tests, verify pass**

Run: `go test ./internal/... -v`
Expected: PASS (all packages)

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/
git commit -m "feat(daemon): events.follow live streaming over the socket"
```

---

### Task 15: Daemon autostart from the client

**Files:**
- Create: `internal/client/ensure.go`

Autostart spawns the current executable as a detached `daemon` process. Unit-testing process spawning is brittle; this is verified end-to-end by the smoke test (Task 17).

- [ ] **Step 1: Implement**

`internal/client/ensure.go`:

```go
package client

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// EnsureDaemon returns a client for the daemon at dir, starting the
// daemon (detached, logging to daemon.log) if it is not running.
func EnsureDaemon(dir, socket string) (*Client, error) {
	c := New(socket)
	if c.ping() == nil {
		return c, nil
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	logf, err := os.OpenFile(filepath.Join(dir, "daemon.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer logf.Close()

	cmd := exec.Command(exe, "daemon")
	cmd.Env = append(os.Environ(), "RUNTIMEPULSE_DIR="+dir)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // survive parent exit
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	cmd.Process.Release()

	for i := 0; i < 30; i++ {
		if c.ping() == nil {
			return c, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, errors.New("daemon did not start; see " + filepath.Join(dir, "daemon.log"))
}

func (c *Client) ping() error {
	var out map[string]any
	return c.Call("status", nil, &out)
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: exits 0

- [ ] **Step 3: Commit**

```bash
git add internal/client/
git commit -m "feat(client): daemon autostart with detached process and readiness poll"
```

---

### Task 16: CLI commands

**Files:**
- Create: `cmd/runtimepulse/main.go`, `cmd_daemon.go`, `cmd_status.go`, `cmd_events.go`, `cmd_rule.go`, `cmd_session.go`, `cmd_inject.go` (all in `cmd/runtimepulse/`)

CLI behavior is exercised by the smoke test (Task 17); no Go unit tests for cobra wiring.

- [ ] **Step 1: Implement root**

`cmd/runtimepulse/main.go`:

```go
// runtimepulse — event-driven session continuation engine for AI coding agents.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tufantunc/RuntimePulse/internal/client"
	"github.com/tufantunc/RuntimePulse/internal/daemon"
)

func main() {
	root := &cobra.Command{
		Use:           "runtimepulse",
		Short:         "Event-driven session continuation engine for AI coding agents",
		Version:       daemon.Version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(daemonCmd(), statusCmd(), eventsCmd(), ruleCmd(), sessionCmd(), injectCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// dial returns a client, auto-starting the daemon if needed.
func dial() (*client.Client, error) {
	dir, err := daemon.StateDir()
	if err != nil {
		return nil, err
	}
	return client.EnsureDaemon(dir, daemon.SocketPath(dir))
}

// printJSON writes v as one JSON line to stdout (spec §7.1 output format).
func printJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
```

- [ ] **Step 2: Implement daemon + status commands**

`cmd/runtimepulse/cmd_daemon.go`:

```go
package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tufantunc/RuntimePulse/internal/daemon"
)

func daemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the RuntimePulse daemon in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := daemon.StateDir()
			if err != nil {
				return err
			}
			d, err := daemon.New(dir)
			if err != nil {
				return fmt.Errorf("starting daemon: %w", err)
			}
			defer d.Close()
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			fmt.Printf("runtimepulse daemon %s listening on %s\n", daemon.Version, daemon.SocketPath(dir))
			return d.Serve(ctx)
		},
	}
}
```

`cmd/runtimepulse/cmd_status.go`:

```go
package main

import "github.com/spf13/cobra"

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var st map[string]any
			if err := c.Call("status", nil, &st); err != nil {
				return err
			}
			return printJSON(st)
		},
	}
}
```

- [ ] **Step 3: Implement events command**

`cmd/runtimepulse/cmd_events.go`:

```go
package main

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/tufantunc/RuntimePulse/internal/core"
)

func eventsCmd() *cobra.Command {
	var follow bool
	var evType string
	var limit int
	cmd := &cobra.Command{
		Use:   "events",
		Short: "List stored events (JSON lines), or stream live with --follow",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			if follow {
				return c.Follow(context.Background(), func(ev core.Event) {
					printJSON(ev)
				})
			}
			var evs []core.Event
			if err := c.Call("events.list",
				map[string]any{"type": evType, "limit": limit}, &evs); err != nil {
				return err
			}
			for _, ev := range evs {
				if err := printJSON(ev); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&follow, "follow", false, "stream events live")
	cmd.Flags().StringVar(&evType, "type", "", "filter by event type")
	cmd.Flags().IntVar(&limit, "limit", 100, "max events to list")
	return cmd
}
```

- [ ] **Step 4: Implement rule command**

`cmd/runtimepulse/cmd_rule.go`:

```go
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func ruleCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "rule", Short: "Manage rules"}
	cmd.AddCommand(ruleAddCmd(), ruleListCmd(), ruleRemoveCmd())
	return cmd
}

func ruleAddCmd() *cobra.Command {
	var on, session, agent, repo, prompt, label string
	var oneShot bool
	var expires time.Duration
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a rule: when an event matches, continue a session",
		RunE: func(cmd *cobra.Command, args []string) error {
			evType, evSource, ok := strings.Cut(on, ":") // "docker.healthy:postgres" or "docker.healthy"
			if !ok {
				evType, evSource = on, ""
			}
			if evType == "" {
				return fmt.Errorf("--on is required (e.g. --on docker.healthy:postgres)")
			}
			params := map[string]any{
				"type": evType, "source": evSource, "sessionId": session,
				"agent": agent, "repoPath": repo, "prompt": prompt,
				"label": label, "oneShot": oneShot,
			}
			if expires > 0 {
				params["expiresAt"] = time.Now().UTC().Add(expires).Format(time.RFC3339)
			}
			c, err := dial()
			if err != nil {
				return err
			}
			var rule map[string]any
			if err := c.Call("rule.add", params, &rule); err != nil {
				return err
			}
			return printJSON(rule)
		},
	}
	cmd.Flags().StringVar(&on, "on", "", "event selector type[:source] (required)")
	cmd.Flags().StringVar(&session, "session", "", "target session id (required)")
	cmd.Flags().StringVar(&agent, "agent", "", "agent type; with --repo, registers the session")
	cmd.Flags().StringVar(&repo, "repo", "", "session repo path; with --agent, registers the session")
	cmd.Flags().StringVar(&prompt, "prompt", "", "prompt template (required)")
	cmd.Flags().StringVar(&label, "label", "", "rule label (becomes continuation event source)")
	cmd.Flags().BoolVar(&oneShot, "one-shot", false, "consume the rule after first match")
	cmd.Flags().DurationVar(&expires, "expires", 0, "rule TTL, e.g. 30m (0 = never)")
	cmd.MarkFlagRequired("on")
	cmd.MarkFlagRequired("session")
	cmd.MarkFlagRequired("prompt")
	return cmd
}

func ruleListCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List rules (JSON lines)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var rules []map[string]any
			if err := c.Call("rule.list", map[string]any{"all": all}, &rules); err != nil {
				return err
			}
			for _, r := range rules {
				if err := printJSON(r); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include consumed and expired rules")
	return cmd
}

func ruleRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <rule-id>",
		Short: "Remove a rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			return c.Call("rule.remove", map[string]any{"id": args[0]}, nil)
		},
	}
}
```

- [ ] **Step 5: Implement session + inject commands**

`cmd/runtimepulse/cmd_session.go`:

```go
package main

import "github.com/spf13/cobra"

func sessionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "session", Short: "Manage the session registry"}
	cmd.AddCommand(sessionRegisterCmd(), sessionListCmd())
	return cmd
}

func sessionRegisterCmd() *cobra.Command {
	var agent, session, repo string
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register an agent session",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var out map[string]any
			if err := c.Call("session.register", map[string]any{
				"sessionId": session, "agent": agent, "repoPath": repo,
			}, &out); err != nil {
				return err
			}
			return printJSON(out)
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent type: claude|cursor|codex|opencode (required)")
	cmd.Flags().StringVar(&session, "session", "", "session id (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "repository path (required)")
	cmd.MarkFlagRequired("agent")
	cmd.MarkFlagRequired("session")
	cmd.MarkFlagRequired("repo")
	return cmd
}

func sessionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered sessions (JSON lines)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var sessions []map[string]any
			if err := c.Call("session.list", nil, &sessions); err != nil {
				return err
			}
			for _, s := range sessions {
				if err := printJSON(s); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
```

`cmd/runtimepulse/cmd_inject.go`:

```go
package main

import "github.com/spf13/cobra"

// inject feeds a synthetic event into the engine — for tests, scripts,
// and CI systems that want to emit events without a watcher.
func injectCmd() *cobra.Command {
	var evType, source string
	var payload map[string]string
	cmd := &cobra.Command{
		Use:   "inject",
		Short: "Inject an event (testing / external producers)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var res map[string]any
			if err := c.Call("event.inject", map[string]any{
				"type": evType, "source": source, "payload": payload,
			}, &res); err != nil {
				return err
			}
			return printJSON(res)
		},
	}
	cmd.Flags().StringVar(&evType, "type", "", "event type (required)")
	cmd.Flags().StringVar(&source, "source", "", "event source")
	cmd.Flags().StringToStringVar(&payload, "payload", nil, "payload k=v pairs")
	cmd.MarkFlagRequired("type")
	return cmd
}
```

- [ ] **Step 6: Verify build and help output**

Run: `go build ./... && go run ./cmd/runtimepulse --help`
Expected: builds; help lists daemon, status, events, rule, session, inject

- [ ] **Step 7: Commit**

```bash
git add cmd/
git commit -m "feat(cli): cobra commands — daemon, status, events, rule, session, inject"
```

---

### Task 17: End-to-end smoke test

**Files:**
- Create: `scripts/smoke.sh`

- [ ] **Step 1: Write the script**

```bash
#!/usr/bin/env bash
# Smoke test: daemon up → session registered → rule added → event injected
# → pending continuation exists → follow stream works → idempotent re-inject.
set -euo pipefail

dir=$(mktemp -d /tmp/rp-smoke.XXXXXX)
trap 'kill $dpid 2>/dev/null || true; rm -rf "$dir"' EXIT
export RUNTIMEPULSE_DIR="$dir"

go build -o "$dir/runtimepulse" ./cmd/runtimepulse
rp="$dir/runtimepulse"

"$rp" daemon >"$dir/daemon-out.log" 2>&1 &
dpid=$!
sleep 0.5

"$rp" session register --agent claude --session smoke-1 --repo "$dir"
"$rp" rule add --on docker.healthy:postgres --session smoke-1 \
  --prompt 'Postgres ({{.Event.Source}}) is healthy. Continue.' --one-shot --label step-1

"$rp" events --follow >"$dir/follow.jsonl" &
fpid=$!
sleep 0.3

"$rp" inject --type docker.healthy --source postgres --payload container=postgres

status=$("$rp" status)
echo "$status"
echo "$status" | grep -q '"pendingContinuations":1' || { echo "FAIL: expected 1 pending continuation"; exit 1; }

# oneShot: same event again must not create a second continuation
"$rp" inject --type docker.healthy --source postgres
status=$("$rp" status)
echo "$status" | grep -q '"pendingContinuations":1' || { echo "FAIL: oneShot consumed twice"; exit 1; }

sleep 0.3
kill $fpid 2>/dev/null || true
grep -q 'docker.healthy' "$dir/follow.jsonl" || { echo "FAIL: follow stream empty"; exit 1; }

echo "SMOKE OK"
```

- [ ] **Step 2: Make executable and run**

Run: `chmod +x scripts/smoke.sh && ./scripts/smoke.sh`
Expected: prints status JSON twice, then `SMOKE OK`, exit 0

- [ ] **Step 3: Run the full test suite one last time**

Run: `go test ./... && go vet ./...`
Expected: PASS, no vet findings

- [ ] **Step 4: Commit**

```bash
git add scripts/smoke.sh
git commit -m "test: end-to-end smoke script for the core engine"
```

---

## Self-Review Notes

- **Spec coverage (this plan's scope):** outbox transaction (§5 steps 1–2) → Task 10; bus → Task 11; daemon+socket+0600 (§3, §9) → Task 13; sessions two registration paths (§4.4: `rule.add` with agent+repoPath = self-registration shape the MCP tool will reuse; `session register` = manual) → Tasks 13/16; JSON-lines event output (§7.1) → Task 16; initial-check/edge semantics belong to watchers (next plan); dispatcher recovery of pending continuations on restart (§5 step 6) belongs to the dispatcher plan — continuations stop at `pending` here by design.
- **Type consistency check:** `Store` methods used by daemon (ListEvents/ListRules/ListSessions/ListContinuations/RegisterSession/GetSession/AddRule/RemoveRule/Ingest) are all defined in Tasks 6–10; `client.New/Call/Follow/EnsureDaemon` match usage in Tasks 14–16; `daemon.Version/StateDir/SocketPath` defined in Task 13 and used in Task 16.
- **Known macOS constraint:** unix socket paths ≤ ~104 bytes — tests use `/tmp`-rooted dirs, daemon default `~/.runtimepulse` is short.
```
