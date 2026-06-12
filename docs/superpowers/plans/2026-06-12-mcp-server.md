# RuntimePulse MCP Server Implementation Plan (Stage 4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose RuntimePulse to AI coding agents as an MCP server — five tools (`create_watch`, `create_rule`, `wait_for_event`, `get_events`, `cancel_rule`) over stdio — so an agent can register "wake me when X" from inside its own session (the spec's primary self-registration flow, §4.4/§7.2).

**Architecture:** `internal/mcpserver` builds an `*mcp.Server` from the official `github.com/modelcontextprotocol/go-sdk` (v1.6.1, typed `mcp.AddTool`). Every tool handler is a **thin client**: it dials the daemon over the existing unix socket via `internal/client` and calls an existing RPC — no new daemon logic except where noted. `runtimepulse mcp` is a new subcommand that `EnsureDaemon`s, builds the server with a client, and runs it over `StdioTransport`. `wait_for_event` is the one blocking tool: it consumes the daemon's live `events.follow` stream, returns the first matching event, or times out (live-only semantics — owner decision 2026-06-12).

**Decisions binding this plan:**
- Official SDK `github.com/modelcontextprotocol/go-sdk@v1.6.1`, **typed `mcp.AddTool`** so a handler's Go error becomes a model-visible tool error, not a dropped connection. See `docs/reference/go-mcp-sdk.md`.
- Tool → RPC: `create_watch`→`watch.add`, `create_rule`→`rule.add` (self-registration via agent+repoPath), `cancel_rule`→`rule.remove`, `get_events`→`events.list`, `wait_for_event`→client-side filter over `events.follow`.
- `wait_for_event` is **live-only**: it returns a matching event published *after* the call, or a timed-out result. History/already-true checking is out of scope; the recommended "is it already ready?" path is `create_watch` (initial-check) + `create_rule` (release-and-resume).
- **stdout belongs to the MCP protocol** — the `mcp` subcommand logs only to stderr.
- Obtaining the caller's own `sessionId` is the caller's responsibility (passed as a tool arg); auto-detection is a documented future gap, not in scope.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk/mcp`, existing `internal/client`, cobra.

**File structure:**

```
internal/mcpserver/server.go        — New(*client.Client) *mcp.Server; tool input/output structs
internal/mcpserver/tools.go         — the five tool handlers (closures over *client.Client)
internal/mcpserver/tools_test.go    — handler tests against a real test daemon
internal/mcpserver/roundtrip_test.go— one in-memory client↔server round trip
cmd/runtimepulse/cmd_mcp.go         — `runtimepulse mcp` subcommand (stdio)
cmd/runtimepulse/main.go            — register mcpCmd (modify)
scripts/smoke.sh                    — launch `runtimepulse mcp`, drive tools/list + a tool (modify)
docs/PROJECT.md                     — §13 progress mark + §7.2 note (modify)
```

A test helper is reused across tasks: the daemon package's `startTestDaemon`/`shortTempDir` are in `internal/daemon` (different package), so `internal/mcpserver` tests spin up their own daemon. Add a local helper `newTestDaemonClient(t)` in Task 2's test file that mirrors the daemon test setup but returns a `*client.Client`.

---

### Task 1: Dependency + server skeleton

**Files:**
- Modify: `go.mod`
- Create: `internal/mcpserver/server.go`
- Test: `internal/mcpserver/server_test.go`

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/modelcontextprotocol/go-sdk@v1.6.1`
Then `go mod tidy`. Confirm cobra/sqlite/fsnotify/x-sys remain in go.mod (they're imported, so they stick).

- [ ] **Step 2: Write the failing test**

`internal/mcpserver/server_test.go`:

```go
package mcpserver

import (
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/client"
)

func TestNewBuildsServer(t *testing.T) {
	srv := New(client.New("/nonexistent.sock"))
	if srv == nil {
		t.Fatal("New must return a server")
	}
}
```

Run: `go test ./internal/mcpserver/ -v` → FAIL (package/New undefined)

- [ ] **Step 3: Implement the skeleton**

`internal/mcpserver/server.go`:

```go
// Package mcpserver exposes RuntimePulse to AI agents as an MCP server.
// Every tool is a thin client over the daemon's unix socket; the server
// itself holds no state.
package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tufantunc/RuntimePulse/internal/client"
)

// Version is reported to MCP clients during initialize.
const Version = "0.1.0-dev"

// New builds the MCP server. Tools are registered in registerTools
// (Tasks 2–4); the skeleton registers none yet.
func New(c *client.Client) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "runtimepulse", Version: Version}, nil)
	registerTools(srv, c)
	return srv
}

// registerTools wires the five tools; filled in over Tasks 2–4.
func registerTools(srv *mcp.Server, c *client.Client) {}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/mcpserver/ -v && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/mcpserver/
git commit -m "feat(mcp): add official go-sdk and mcp server skeleton"
```

---

### Task 2: Thin pass-through tools — create_watch, get_events, cancel_rule

**Files:**
- Create: `internal/mcpserver/tools.go`
- Test: `internal/mcpserver/tools_test.go`

These three are direct RPC pass-throughs. Handlers are closures over the client so they're testable without the stdio transport.

- [ ] **Step 1: Write the failing test**

`internal/mcpserver/tools_test.go`:

```go
package mcpserver

import (
	"context"
	"os"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/client"
	"github.com/tufantunc/RuntimePulse/internal/daemon"
)

// newTestDaemonClient starts a daemon in a short tmp dir and returns a
// connected client. macOS unix socket paths are ~104 bytes, so use /tmp.
func newTestDaemonClient(t *testing.T) *client.Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rpmcp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	d, err := daemon.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); d.Close() })
	go d.Serve(ctx)
	return client.New(daemon.SocketPath(dir))
}

func TestCreateWatchTool(t *testing.T) {
	c := newTestDaemonClient(t)
	h := createWatchHandler(c)
	_, out, err := h(context.Background(), nil, CreateWatchInput{
		Type: "tcp", Target: "localhost:5432", Interval: "1s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID == "" || out.Type != "tcp" {
		t.Fatalf("watch not created: %#v", out)
	}
	// invalid type → tool error (non-nil err)
	if _, _, err := h(context.Background(), nil, CreateWatchInput{Type: "nope", Target: "x"}); err == nil {
		t.Fatal("invalid watch type must surface as a tool error")
	}
}

func TestGetEventsTool(t *testing.T) {
	c := newTestDaemonClient(t)
	if err := c.Call("event.inject", map[string]any{"type": "build.failed", "source": "ci"}, nil); err != nil {
		t.Fatal(err)
	}
	h := getEventsHandler(c)
	_, out, err := h(context.Background(), nil, GetEventsInput{Type: "build.failed", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 1 || out.Events[0].Type != "build.failed" {
		t.Fatalf("get_events wrong: %#v", out.Events)
	}
}

func TestCancelRuleTool(t *testing.T) {
	c := newTestDaemonClient(t)
	if err := c.Call("session.register",
		map[string]any{"sessionId": "s1", "agent": "claude", "repoPath": "/tmp"}, nil); err != nil {
		t.Fatal(err)
	}
	var rule map[string]any
	if err := c.Call("rule.add", map[string]any{
		"type": "tcp.available", "sessionId": "s1", "prompt": "go",
	}, &rule); err != nil {
		t.Fatal(err)
	}
	id, _ := rule["id"].(string)
	h := cancelRuleHandler(c)
	if _, _, err := h(context.Background(), nil, CancelRuleInput{RuleID: id}); err != nil {
		t.Fatal(err)
	}
	var rules []map[string]any
	if err := c.Call("rule.list", map[string]any{"all": false}, &rules); err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("rule not cancelled: %v", rules)
	}
}
```

Run: `go test ./internal/mcpserver/ -run 'TestCreateWatch|TestGetEvents|TestCancelRule' -v` → FAIL

- [ ] **Step 2: Implement**

`internal/mcpserver/tools.go`:

```go
package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tufantunc/RuntimePulse/internal/core"
	"github.com/tufantunc/RuntimePulse/internal/client"
)

// --- create_watch -----------------------------------------------------

type CreateWatchInput struct {
	Type      string `json:"type" jsonschema:"watch type: http, tcp, file, process, docker or git"`
	Target    string `json:"target" jsonschema:"what to watch (url, host:port, path, pgrep pattern, container, or repo)"`
	Interval  string `json:"interval,omitempty" jsonschema:"poll interval as a Go duration, e.g. 2s"`
	Stability string `json:"stability,omitempty" jsonschema:"flap-suppression threshold as a Go duration, e.g. 5s"`
}

type WatchOutput struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Target string `json:"target"`
}

func createWatchHandler(c *client.Client) mcp.ToolHandlerFor[CreateWatchInput, WatchOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CreateWatchInput) (*mcp.CallToolResult, WatchOutput, error) {
		var out WatchOutput
		err := c.Call("watch.add", map[string]any{
			"type": in.Type, "target": in.Target,
			"interval": in.Interval, "stability": in.Stability,
		}, &out)
		return nil, out, err
	}
}

// --- get_events -------------------------------------------------------

type GetEventsInput struct {
	Type  string `json:"type,omitempty" jsonschema:"filter by event type, e.g. docker.healthy"`
	Limit int    `json:"limit,omitempty" jsonschema:"max events to return (default 100)"`
}

type GetEventsOutput struct {
	Events []core.Event `json:"events"`
}

func getEventsHandler(c *client.Client) mcp.ToolHandlerFor[GetEventsInput, GetEventsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetEventsInput) (*mcp.CallToolResult, GetEventsOutput, error) {
		var out GetEventsOutput
		err := c.Call("events.list", map[string]any{"type": in.Type, "limit": in.Limit}, &out.Events)
		return nil, out, err
	}
}

// --- cancel_rule ------------------------------------------------------

type CancelRuleInput struct {
	RuleID string `json:"ruleId" jsonschema:"id of the rule to cancel"`
}

type OKOutput struct {
	OK bool `json:"ok"`
}

func cancelRuleHandler(c *client.Client) mcp.ToolHandlerFor[CancelRuleInput, OKOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CancelRuleInput) (*mcp.CallToolResult, OKOutput, error) {
		if err := c.Call("rule.remove", map[string]any{"id": in.RuleID}, nil); err != nil {
			return nil, OKOutput{}, err
		}
		return nil, OKOutput{OK: true}, nil
	}
}
```

Wire them in `registerTools` (server.go):

```go
func registerTools(srv *mcp.Server, c *client.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "create_watch", Description: "Watch a runtime condition; emits events on state transitions.",
	}, createWatchHandler(c))
	mcp.AddTool(srv, &mcp.Tool{
		Name: "get_events", Description: "Query recent events, newest first, optionally filtered by type.",
	}, getEventsHandler(c))
	mcp.AddTool(srv, &mcp.Tool{
		Name: "cancel_rule", Description: "Cancel a pending rule by id.",
	}, cancelRuleHandler(c))
}
```

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/mcpserver/ -v && go vet ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/mcpserver/
git commit -m "feat(mcp): create_watch, get_events and cancel_rule tools"
```

---

### Task 3: create_rule tool (self-registration)

**Files:**
- Modify: `internal/mcpserver/tools.go`, `internal/mcpserver/server.go`
- Test: `internal/mcpserver/tools_test.go` (append)

`create_rule` is the primary flow: an agent registers itself (agent+repoPath) and binds an event to a resume prompt in one call (the daemon's `rule.add` already does this).

- [ ] **Step 1: Write the failing test (append)**

```go
func TestCreateRuleSelfRegisters(t *testing.T) {
	c := newTestDaemonClient(t)
	h := createRuleHandler(c)
	_, out, err := h(context.Background(), nil, CreateRuleInput{
		EventType: "docker.healthy", Source: "postgres",
		SessionID: "abc123", Agent: "claude", RepoPath: "/tmp/x",
		Prompt: "{{.Event.Source}} healthy. Continue.", Label: "step-1", OneShot: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID == "" {
		t.Fatalf("rule not created: %#v", out)
	}
	// session was registered as a side effect
	var sessions []map[string]any
	if err := c.Call("session.list", nil, &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0]["sessionId"] != "abc123" {
		t.Fatalf("self-registration failed: %v", sessions)
	}
}

func TestCreateRuleBadTemplateIsToolError(t *testing.T) {
	c := newTestDaemonClient(t)
	h := createRuleHandler(c)
	_, _, err := h(context.Background(), nil, CreateRuleInput{
		EventType: "docker.healthy", SessionID: "abc123", Agent: "claude", RepoPath: "/tmp/x",
		Prompt: "{{.Event.Bad", // syntax error
	})
	if err == nil {
		t.Fatal("bad template must surface as a tool error")
	}
}
```

Run: `go test ./internal/mcpserver/ -run TestCreateRule -v` → FAIL

- [ ] **Step 2: Implement (append to tools.go)**

```go
// --- create_rule ------------------------------------------------------

type CreateRuleInput struct {
	EventType string `json:"eventType" jsonschema:"event type to match, e.g. docker.healthy or continuation.completed"`
	Source    string `json:"source,omitempty" jsonschema:"optional event source filter, e.g. a container name"`
	SessionID string `json:"sessionId" jsonschema:"the agent session to resume when the event matches"`
	Agent     string `json:"agent,omitempty" jsonschema:"agent type (claude|cursor|codex|opencode); with repoPath, self-registers the session"`
	RepoPath  string `json:"repoPath,omitempty" jsonschema:"session repo path; with agent, self-registers the session"`
	Prompt    string `json:"prompt" jsonschema:"Go text/template prompt; only event fields are available, e.g. {{.Event.Source}}"`
	Label     string `json:"label,omitempty" jsonschema:"rule label; becomes the source of the continuation.* result event"`
	OneShot   bool   `json:"oneShot,omitempty" jsonschema:"consume the rule after its first match"`
	ExpiresAt string `json:"expiresAt,omitempty" jsonschema:"optional RFC3339 expiry"`
}

type RuleOutput struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
}

func createRuleHandler(c *client.Client) mcp.ToolHandlerFor[CreateRuleInput, RuleOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CreateRuleInput) (*mcp.CallToolResult, RuleOutput, error) {
		var out RuleOutput
		err := c.Call("rule.add", map[string]any{
			"type": in.EventType, "source": in.Source,
			"sessionId": in.SessionID, "agent": in.Agent, "repoPath": in.RepoPath,
			"prompt": in.Prompt, "label": in.Label, "oneShot": in.OneShot, "expiresAt": in.ExpiresAt,
		}, &out)
		return nil, out, err
	}
}
```

Add to `registerTools`:

```go
	mcp.AddTool(srv, &mcp.Tool{
		Name: "create_rule",
		Description: "Bind an event to a session resume: when eventType (optionally from source) fires, " +
			"resume sessionId with the rendered prompt. Pass agent+repoPath to register the session in the same call.",
	}, createRuleHandler(c))
```

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/mcpserver/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/mcpserver/
git commit -m "feat(mcp): create_rule tool with session self-registration"
```

---

### Task 4: wait_for_event tool (live-only, timeout)

**Files:**
- Modify: `internal/mcpserver/tools.go`, `internal/mcpserver/server.go`
- Test: `internal/mcpserver/tools_test.go` (append)

Live-only: subscribe to the daemon stream, return the first event matching `eventType` (and `source` if given) published after the call, or a timed-out result. Uses the existing `client.Follow`.

- [ ] **Step 1: Write the failing test (append)**

```go
func TestWaitForEventReturnsLiveMatch(t *testing.T) {
	c := newTestDaemonClient(t)
	h := waitForEventHandler(c)

	got := make(chan WaitOutput, 1)
	go func() {
		_, out, err := h(context.Background(), nil, WaitForEventInput{
			EventType: "http.available", Source: "localhost:3000", TimeoutSeconds: 5,
		})
		if err != nil {
			t.Errorf("wait_for_event: %v", err)
		}
		got <- out
	}()

	time.Sleep(150 * time.Millisecond) // let the subscription arm
	if err := c.Call("event.inject",
		map[string]any{"type": "http.available", "source": "localhost:3000"}, nil); err != nil {
		t.Fatal(err)
	}

	select {
	case out := <-got:
		if out.TimedOut || out.Event.Type != "http.available" || out.Event.Source != "localhost:3000" {
			t.Fatalf("unexpected wait result: %#v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("wait_for_event did not return on a live match")
	}
}

func TestWaitForEventIgnoresNonMatching() {} // placeholder removed below

func TestWaitForEventTimesOut(t *testing.T) {
	c := newTestDaemonClient(t)
	h := waitForEventHandler(c)
	_, out, err := h(context.Background(), nil, WaitForEventInput{
		EventType: "never.happens", TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.TimedOut {
		t.Fatalf("expected timeout, got %#v", out)
	}
}

func TestWaitForEventFiltersSource(t *testing.T) {
	c := newTestDaemonClient(t)
	h := waitForEventHandler(c)
	got := make(chan WaitOutput, 1)
	go func() {
		_, out, _ := h(context.Background(), nil, WaitForEventInput{
			EventType: "docker.healthy", Source: "postgres", TimeoutSeconds: 5,
		})
		got <- out
	}()
	time.Sleep(150 * time.Millisecond)
	c.Call("event.inject", map[string]any{"type": "docker.healthy", "source": "redis"}, nil)   // non-match
	c.Call("event.inject", map[string]any{"type": "docker.healthy", "source": "postgres"}, nil) // match
	select {
	case out := <-got:
		if out.TimedOut || out.Event.Source != "postgres" {
			t.Fatalf("source filter failed: %#v", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("wait_for_event did not return")
	}
}
```

(delete the empty `TestWaitForEventIgnoresNonMatching` placeholder line; add `"time"` to the test imports)

Run: `go test ./internal/mcpserver/ -run TestWaitForEvent -v` → FAIL

- [ ] **Step 2: Implement (append to tools.go; add imports `time`, `github.com/tufantunc/RuntimePulse/internal/core`)**

```go
// --- wait_for_event ---------------------------------------------------

type WaitForEventInput struct {
	EventType      string `json:"eventType" jsonschema:"event type to wait for, e.g. http.available"`
	Source         string `json:"source,omitempty" jsonschema:"optional source filter"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty" jsonschema:"max seconds to block (default 300)"`
}

type WaitOutput struct {
	TimedOut bool       `json:"timedOut"`
	Event    core.Event `json:"event,omitempty"`
}

const defaultWaitTimeout = 300 * time.Second

// waitForEventHandler blocks until a matching event is published on the
// daemon bus (LIVE only — events from before the call are not seen) or
// the timeout elapses. The "is it already ready?" case is served by
// create_watch (initial-check) + create_rule (release-and-resume).
func waitForEventHandler(c *client.Client) mcp.ToolHandlerFor[WaitForEventInput, WaitOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in WaitForEventInput) (*mcp.CallToolResult, WaitOutput, error) {
		timeout := defaultWaitTimeout
		if in.TimeoutSeconds > 0 {
			timeout = time.Duration(in.TimeoutSeconds) * time.Second
		}
		waitCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		var match core.Event
		found := false
		// Follow returns when waitCtx is cancelled (timeout or first match).
		err := c.Follow(waitCtx, func(ev core.Event) {
			if found {
				return
			}
			if ev.Type == in.EventType && (in.Source == "" || ev.Source == in.Source) {
				match, found = ev, true
				cancel() // stop the stream
			}
		})
		// A cancelled Follow returns nil (see client.Follow); a real
		// transport error is reported as a tool error.
		if err != nil {
			return nil, WaitOutput{}, err
		}
		if !found {
			return nil, WaitOutput{TimedOut: true}, nil
		}
		return nil, WaitOutput{Event: match}, nil
	}
}
```

Add to `registerTools`:

```go
	mcp.AddTool(srv, &mcp.Tool{
		Name: "wait_for_event",
		Description: "Block until a matching event is published (live only; events before the call are not seen), " +
			"or until timeoutSeconds elapses. For 'is it already ready?', prefer create_watch + create_rule.",
	}, waitForEventHandler(c))
```

> Implementation note: confirm `client.Follow` returns nil error on `ctx` cancellation (it does in stage 1 — `if ctx.Err() != nil { return nil }`). If a match arrives and we `cancel()`, Follow's next decode errors with the closed connection but ctx is already cancelled, so it returns nil. The `found` flag guards against a second match in the same buffered batch.

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/mcpserver/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/mcpserver/
git commit -m "feat(mcp): wait_for_event tool with live-stream filtering and timeout"
```

---

### Task 5: `runtimepulse mcp` subcommand

**Files:**
- Create: `cmd/runtimepulse/cmd_mcp.go`
- Modify: `cmd/runtimepulse/main.go`

- [ ] **Step 1: Implement**

`cmd/runtimepulse/cmd_mcp.go`:

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/tufantunc/RuntimePulse/internal/daemon"
	"github.com/tufantunc/RuntimePulse/internal/mcpserver"
)

// mcp runs the MCP server over stdio. It is launched by an agent CLI,
// e.g.  claude mcp add --transport stdio runtimepulse -- runtimepulse mcp
// stdout carries the MCP protocol stream; all logging goes to stderr.
func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the RuntimePulse MCP server over stdio",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.SetOutput(os.Stderr) // never write to stdout: it is the protocol stream
			c, err := dial()         // EnsureDaemon: auto-starts the shared daemon
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return mcpserver.New(c).Run(ctx, &mcp.StdioTransport{})
		},
	}
}
```

`main.go`: append `mcpCmd()` to the `AddCommand` call.

- [ ] **Step 2: Verify build + help + that it doesn't pollute stdout on startup**

Run: `go build ./... && go run ./cmd/runtimepulse --help | grep -q mcp && echo OK`
Expected: builds; `mcp` listed; `OK`

- [ ] **Step 3: Commit**

```bash
git add cmd/
git commit -m "feat(cli): runtimepulse mcp stdio subcommand"
```

---

### Task 6: In-memory round-trip test

**Files:**
- Create: `internal/mcpserver/roundtrip_test.go`

Drive the server through a real MCP client over the SDK's in-memory transport — proves tool registration, schema derivation, and dispatch end to end (not just handler funcs).

- [ ] **Step 1: Confirm the in-memory transport API**

Run: `go doc github.com/modelcontextprotocol/go-sdk/mcp | grep -i 'InMemory\|Transport'`
The SDK provides paired in-memory transports (commonly `mcp.NewInMemoryTransports() (*InMemoryTransport, *InMemoryTransport)`) and a client (`mcp.NewClient` + `client.Connect`). Confirm the exact names with `go doc` before writing the test; adjust the snippet below to match.

- [ ] **Step 2: Write the round-trip test**

```go
package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRoundTripListsToolsAndCreatesWatch(t *testing.T) {
	c := newTestDaemonClient(t)
	srv := New(c)

	// Adjust constructor/method names per `go doc` (Step 1).
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()

	go func() {
		if err := srv.Run(ctx, serverTransport); err != nil {
			t.Errorf("server.Run: %v", err)
		}
	}()

	mc := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	sess, err := mc.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"create_watch": false, "create_rule": false, "wait_for_event": false, "get_events": false, "cancel_rule": false}
	for _, tool := range tools.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("tool %q not advertised", name)
		}
	}

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "create_watch",
		Arguments: map[string]any{"type": "tcp", "target": "localhost:5432", "interval": "1s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("create_watch returned tool error: %+v", res.Content)
	}
	_ = time.Second // keep import if unused after adjustments
}
```

> If the exact client API differs (method names like `Session.ListTools`/`CallTool`, param struct shapes), fix per `go doc` — the handler tests already prove behavior, so this test only needs to prove the wiring/round trip. If the in-memory transport truly isn't available in v1.6.1, fall back to `mcp.NewClient` over a command transport spawning the built binary; if neither is workable, mark this task DONE_WITH_CONCERNS and rely on the smoke check (Task 7).

- [ ] **Step 3: Run, verify pass**

Run: `go test -race ./internal/mcpserver/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/mcpserver/
git commit -m "test(mcp): in-memory client round trip over the real server"
```

---

### Task 7: Smoke + docs

**Files:**
- Modify: `scripts/smoke.sh`, `docs/PROJECT.md`

- [ ] **Step 1: Add an MCP launch check to smoke**

The smoke script already builds `$rp` and runs a daemon. Add, before the final `echo "SMOKE OK"`, a check that `runtimepulse mcp` speaks MCP over stdio. Use a minimal stdin script of newline-delimited JSON-RPC: `initialize`, the `notifications/initialized` notification, then `tools/list`; assert the five tool names appear in stdout.

```bash
# --- MCP server: stdio handshake lists the five tools ---
mcp_in="$dir/mcp_in.jsonl"
cat > "$mcp_in" << 'JSONL'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
JSONL
mcp_out=$("$rp" mcp < "$mcp_in" 2>/dev/null || true)
for tool in create_watch create_rule wait_for_event get_events cancel_rule; do
  echo "$mcp_out" | grep -q "$tool" || { echo "FAIL: MCP tools/list missing $tool"; exit 1; }
done
```

> Note: the server reads stdin until EOF then exits when the pipe closes — feeding a fixed request set and capturing stdout works for a one-shot handshake. If the protocol version string causes an initialize rejection, use the version the SDK reports (check `go doc` or the server's initialize response); the tool-name grep is the actual assertion. If this handshake proves flaky in bash, replace it with a tiny Go helper under `scripts/` or rely on Task 6's round-trip test and downgrade this to a "server starts and exits cleanly" check.

- [ ] **Step 2: Run smoke ×3**

Run: `./scripts/smoke.sh && ./scripts/smoke.sh && ./scripts/smoke.sh`
Expected: `SMOKE OK` all three

- [ ] **Step 3: Docs**

In `docs/PROJECT.md`: §13 item 4 — mark the MCP server half done (✅ MCP server; CLI surface already done in stages 1–3). Add to §7.2 a one-line install note:

```
Agents register the server with: claude mcp add --transport stdio runtimepulse -- runtimepulse mcp
wait_for_event is live-only (blocks for an event published after the call); create_rule is the primary release-and-resume flow.
```

- [ ] **Step 4: Full verification + commit**

Run: `go test -race ./... && go vet ./... && gofmt -l .`
Expected: all green, gofmt empty

```bash
git add scripts/smoke.sh docs/PROJECT.md
git commit -m "test(mcp): stdio tools/list smoke check; document MCP install and wait semantics"
```

---

## Self-Review Notes

- **Spec coverage (§7.2):** all five tools → Tasks 2–4; stdio server + install path → Task 5; the two waiting modes (release-and-resume via create_rule, in-turn via wait_for_event) are documented in tool descriptions and §7.2. MCP self-registration (§4.4 primary path) → create_rule's agent+repoPath.
- **Type consistency:** handlers return the typed `WatchOutput`/`RuleOutput`/`GetEventsOutput`/`OKOutput`/`WaitOutput`; `New`→`registerTools` registers exactly five tools matching the five handler constructors; `client.Call`/`client.Follow` signatures match existing stage-1 client.
- **No daemon changes:** every tool maps to an existing RPC (`watch.add`, `rule.add`, `rule.remove`, `events.list`, `events.follow`). If a test reveals a missing field, prefer fixing the handler mapping over adding daemon RPCs.
- **stdout discipline:** only `mcp.StdioTransport` writes stdout; `log.SetOutput(os.Stderr)` in the subcommand; handler errors are tool errors (typed AddTool), never stdout.
- **Known limitation (documented, not fixed):** the agent must supply its own `sessionId` to create_rule — auto-detection of the calling session is a future gap.
- **SDK API risk:** Tasks 1–5 use only the verified core API (`NewServer`, `AddTool`, `ToolHandlerFor`, `StdioTransport`, `Run`). Task 6's client/in-memory-transport names are the one place to confirm via `go doc` before coding.
