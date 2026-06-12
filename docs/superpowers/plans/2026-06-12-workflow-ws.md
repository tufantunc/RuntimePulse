# RuntimePulse Workflow YAML + WebSocket Streaming Implementation Plan (Stage 6)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete spec §13 Phase 4 (minus Channels — deferred while it's a research preview): `runtimepulse apply workflow.yaml` compiles a declarative step list into oneShot rules (no workflow entity in the core, spec §7.3), and an opt-in WebSocket server streams live events on `127.0.0.1` with a token (spec §9).

**Architecture:** `internal/workflow` is a pure package: YAML → validated `Workflow` → `[]core.Rule` (labels chained via `continuation.completed`). The `apply` CLI command is a thin client: one `rule.add` per step (first call self-registers the session via agent+repoPath), best-effort rollback on partial failure. `internal/wsserver` serves a token-gated `/events` WebSocket bound to loopback only (bind address not configurable by construction); the daemon starts it only when `--ws-port` is given, generating a persistent token file (`ws-token`, 0600) in the state dir.

**Decisions binding this plan:**
- Channels optimization **deferred** (research preview per `docs/reference/claude-code.md` §7) — record in PROJECT.md §13.
- YAML via `gopkg.in/yaml.v3` with `KnownFields(true)` (typos fail loudly). Spec §7.3's shape + a new optional `repo:` field (session registration needs repoPath; defaults to the cwd of `apply`).
- All compiled rules are `oneShot: true` — a workflow run is one pass; re-`apply` re-arms it.
- Apply is client-side ("merely compiles YAML to rules"); partial failure rolls back already-created rules best-effort and errors.
- WebSocket: `github.com/coder/websocket` (minimal, maintained). Default OFF; `127.0.0.1` hardcoded; token via query param or Bearer header, constant-time compare. Bus semantics apply (slow consumer misses events; durability stays in the store).
- Smoke additions follow the SIGPIPE rule (`has` helper, never `| grep -q`).

**Tech Stack:** Go, `gopkg.in/yaml.v3`, `github.com/coder/websocket`, existing internal packages.

**File structure:**

```
internal/workflow/workflow.go     — Parse + Rules + validation
internal/workflow/workflow_test.go
cmd/runtimepulse/cmd_apply.go     — apply command with rollback
internal/wsserver/wsserver.go     — token-gated /events WebSocket
internal/wsserver/wsserver_test.go
internal/daemon/daemon.go         — ServeWS + token file (modify)
cmd/runtimepulse/cmd_daemon.go    — --ws-port flag (modify)
scripts/smoke.sh                  — workflow-apply + ws-401 sections (modify)
docs/PROJECT.md                   — §7.3 repo field, §13 final marks (modify)
```

---

### Task 1: `internal/workflow` — parse and compile

**Files:**
- Create: `internal/workflow/workflow.go`
- Test: `internal/workflow/workflow_test.go`
- Modify: `go.mod` (`go get gopkg.in/yaml.v3`)

- [ ] **Step 1: Add the dependency**

Run: `go get gopkg.in/yaml.v3 && go mod tidy`

- [ ] **Step 2: Write the failing tests**

```go
package workflow

import (
	"strings"
	"testing"
)

const validYAML = `
session: abc123
agent: claude
repo: /tmp/x
steps:
  - label: step-1
    on: { type: docker.healthy, source: postgres }
    prompt: "Postgres is ready. Run the migrations."
  - on: { type: continuation.completed, source: step-1 }
    prompt: "Migrations done. Run the tests."
`

func TestParseAndCompile(t *testing.T) {
	w, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if w.Session != "abc123" || w.Agent != "claude" || w.Repo != "/tmp/x" {
		t.Fatalf("header lost: %#v", w)
	}
	rules := w.Rules()
	if len(rules) != 2 {
		t.Fatalf("got %d rules", len(rules))
	}
	if rules[0].Label != "step-1" || rules[1].Label != "step-2" {
		t.Fatalf("labels: %q %q (second must default to step-N)", rules[0].Label, rules[1].Label)
	}
	if !rules[0].OneShot || !rules[1].OneShot {
		t.Fatal("workflow rules must be oneShot")
	}
	if rules[1].Selector.Type != "continuation.completed" || rules[1].Selector.Source != "step-1" {
		t.Fatalf("chain selector lost: %#v", rules[1].Selector)
	}
	if rules[0].SessionID != "abc123" {
		t.Fatalf("session not propagated: %#v", rules[0])
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	cases := []struct{ name, yaml, wantErr string }{
		{"missing session", "agent: claude\nsteps:\n  - on: {type: x}\n    prompt: p\n", "session"},
		{"missing agent", "session: s\nsteps:\n  - on: {type: x}\n    prompt: p\n", "agent"},
		{"no steps", "session: s\nagent: claude\n", "step"},
		{"missing type", "session: s\nagent: claude\nsteps:\n  - prompt: p\n", "type"},
		{"missing prompt", "session: s\nagent: claude\nsteps:\n  - on: {type: x}\n", "prompt"},
		{"duplicate labels", "session: s\nagent: claude\nsteps:\n  - {label: a, on: {type: x}, prompt: p}\n  - {label: a, on: {type: y}, prompt: p}\n", "duplicate"},
		{"bad template", "session: s\nagent: claude\nsteps:\n  - on: {type: x}\n    prompt: \"{{.Event.Bad\"\n", "template"},
		{"unknown field", "session: s\nagent: claude\nsessoin: typo\nsteps:\n  - on: {type: x}\n    prompt: p\n", "field"},
	}
	for _, c := range cases {
		if _, err := Parse([]byte(c.yaml)); err == nil || !strings.Contains(strings.ToLower(err.Error()), c.wantErr) {
			t.Errorf("%s: err = %v, want mention of %q", c.name, err, c.wantErr)
		}
	}
}
```

Run: `go test ./internal/workflow/ -v` → FAIL

- [ ] **Step 3: Implement**

```go
// Package workflow compiles declarative workflow files into rules.
// There is no workflow entity in the core (spec §7.3): a workflow is
// syntax sugar that emits oneShot rules, chained through
// continuation.completed events.
package workflow

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

type Selector struct {
	Type   string `yaml:"type"`
	Source string `yaml:"source"`
}

type Step struct {
	Label  string   `yaml:"label"`
	On     Selector `yaml:"on"`
	Prompt string   `yaml:"prompt"`
}

type Workflow struct {
	Session string `yaml:"session"`
	Agent   string `yaml:"agent"`
	// Repo is the session's repoPath; empty means "the directory apply
	// runs in" (resolved by the CLI, not here — this package is pure).
	Repo  string `yaml:"repo"`
	Steps []Step `yaml:"steps"`
}

// Parse decodes and validates a workflow document. Unknown YAML fields
// are errors (typo protection); missing labels default to step-N;
// prompt templates are syntax-checked (execution errors surface later
// as failed continuations, same contract as rule.add).
func Parse(data []byte) (Workflow, error) {
	var w Workflow
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&w); err != nil {
		return w, fmt.Errorf("workflow: bad field or syntax: %w", err)
	}
	if w.Session == "" {
		return w, fmt.Errorf("workflow: session is required")
	}
	if w.Agent == "" {
		return w, fmt.Errorf("workflow: agent is required")
	}
	if len(w.Steps) == 0 {
		return w, fmt.Errorf("workflow: at least one step is required")
	}
	seen := map[string]bool{}
	for i := range w.Steps {
		s := &w.Steps[i]
		if s.Label == "" {
			s.Label = fmt.Sprintf("step-%d", i+1)
		}
		if seen[s.Label] {
			return w, fmt.Errorf("workflow: duplicate label %q", s.Label)
		}
		seen[s.Label] = true
		if s.On.Type == "" {
			return w, fmt.Errorf("workflow: step %q: on.type is required", s.Label)
		}
		if s.Prompt == "" {
			return w, fmt.Errorf("workflow: step %q: prompt is required", s.Label)
		}
		if err := core.ValidatePromptTemplate(s.Prompt); err != nil {
			return w, fmt.Errorf("workflow: step %q: bad prompt template: %w", s.Label, err)
		}
	}
	return w, nil
}

// Rules compiles the workflow into oneShot rules.
func (w Workflow) Rules() []core.Rule {
	out := make([]core.Rule, 0, len(w.Steps))
	for _, s := range w.Steps {
		out = append(out, core.Rule{
			Selector:       core.EventSelector{Type: s.On.Type, Source: s.On.Source},
			ActionKind:     core.ActionContinueSession,
			SessionID:      w.Session,
			PromptTemplate: s.Prompt,
			Label:          s.Label,
			OneShot:        true,
		})
	}
	return out
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test -race ./internal/workflow/ -v && go vet ./... && gofmt -l .`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/workflow/
git commit -m "feat(workflow): yaml parser compiling declarative steps to oneShot rules"
```

---

### Task 2: `runtimepulse apply` + workflow smoke

**Files:**
- Create: `cmd/runtimepulse/cmd_apply.go`
- Modify: `cmd/runtimepulse/main.go` (register), `scripts/smoke.sh`

- [ ] **Step 1: Implement the command**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/tufantunc/RuntimePulse/internal/workflow"
)

// apply compiles a workflow file to rules (spec §7.3). The first
// rule.add self-registers the session (agent+repoPath). On a partial
// failure the already-created rules are removed best-effort, so a bad
// step never leaves half a workflow armed.
func applyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply <workflow.yaml>",
		Short: "Compile a workflow file into rules",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			w, err := workflow.Parse(data)
			if err != nil {
				return err
			}
			repo := w.Repo
			if repo == "" {
				if repo, err = os.Getwd(); err != nil {
					return err
				}
			}
			abs, err := filepath.Abs(repo)
			if err != nil {
				return err
			}
			c, err := dial()
			if err != nil {
				return err
			}
			var created []string
			for _, r := range w.Rules() {
				var out map[string]any
				if err := c.Call("rule.add", map[string]any{
					"type": r.Selector.Type, "source": r.Selector.Source,
					"sessionId": r.SessionID, "agent": w.Agent, "repoPath": abs,
					"prompt": r.PromptTemplate, "label": r.Label, "oneShot": true,
				}, &out); err != nil {
					for _, id := range created {
						c.Call("rule.remove", map[string]any{"id": id}, nil) // best-effort rollback
					}
					return fmt.Errorf("apply: step %q failed: %w (rolled back %d created rules)",
						r.Label, err, len(created))
				}
				if id, ok := out["id"].(string); ok {
					created = append(created, id)
				}
				printJSON(out)
			}
			return nil
		},
	}
}
```

Register `applyCmd()` in main.go's `AddCommand`.

- [ ] **Step 2: Smoke — workflow section**

In `scripts/smoke.sh`, after the multi-agent section (before MCP), add (REMEMBER: `has` helper, never `| grep -q`):

```bash
# --- workflow apply: yaml compiles to a chained two-step run ---
cat > "$dir/wf.yaml" << WF
session: smoke-wf
agent: claude
repo: $dir
steps:
  - label: wf-1
    on: { type: exec.succeeded, source: wf-build }
    prompt: "Build done. Step one."
  - label: wf-2
    on: { type: continuation.completed, source: wf-1 }
    prompt: "Step one done. Step two."
WF
"$rp" apply "$dir/wf.yaml"
"$rp" exec --label wf-build -- true
wfdone=""
for _ in $(seq 1 50); do
  if has wf-2 "$rp" events --type continuation.completed; then wfdone=1; break; fi
  sleep 0.2
done
[ -n "$wfdone" ] || { echo "FAIL: workflow chain did not complete"; exit 1; }

# a bad workflow must not leave partial rules behind
rules_before=$(count '"id"' "$rp" rule list)
cat > "$dir/bad.yaml" << WF
session: smoke-wf
agent: claude
steps:
  - on: { type: x }
    prompt: "{{.Event.Bad"
WF
if "$rp" apply "$dir/bad.yaml" 2>/dev/null; then echo "FAIL: bad workflow must error"; exit 1; fi
rules_after=$(count '"id"' "$rp" rule list)
[ "$rules_before" -eq "$rules_after" ] || { echo "FAIL: bad apply leaked rules ($rules_before -> $rules_after)"; exit 1; }
```

- [ ] **Step 3: Verify**

Run: `go build ./... && ./scripts/smoke.sh && ./scripts/smoke.sh && go vet ./... && gofmt -l .`
Expected: builds, `SMOKE OK` ×2

- [ ] **Step 4: Commit**

```bash
git add cmd/ scripts/smoke.sh
git commit -m "feat(cli): apply command compiles workflow yaml to rules with rollback"
```

---

### Task 3: `internal/wsserver` — token-gated event stream

**Files:**
- Create: `internal/wsserver/wsserver.go`
- Test: `internal/wsserver/wsserver_test.go`
- Modify: `go.mod` (`go get github.com/coder/websocket`)

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/coder/websocket && go mod tidy`

- [ ] **Step 2: Write the failing tests**

```go
package wsserver

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/tufantunc/RuntimePulse/internal/bus"
	"github.com/tufantunc/RuntimePulse/internal/core"
)

func startTestWS(t *testing.T) (string, *bus.Bus) {
	t.Helper()
	b := bus.New()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	addr, err := Start(ctx, 0, "sekrit", b) // port 0 = ephemeral
	if err != nil {
		t.Fatal(err)
	}
	return addr, b
}

func TestStreamDeliversBusEvents(t *testing.T) {
	addr, b := startTestWS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://%s/events?token=sekrit", addr), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	time.Sleep(100 * time.Millisecond) // let the subscription arm
	b.Publish(core.Event{ID: "evt-1", Type: "tcp.available", Source: "x"})

	var ev core.Event
	if err := wsjson.Read(ctx, conn, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.ID != "evt-1" || ev.Type != "tcp.available" {
		t.Fatalf("wrong event: %#v", ev)
	}
}

func TestWrongTokenRejected(t *testing.T) {
	addr, _ := startTestWS(t)
	resp, err := http.Get(fmt.Sprintf("http://%s/events?token=wrong", addr))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token must 401, got %d", resp.StatusCode)
	}
	resp, err = http.Get(fmt.Sprintf("http://%s/events", addr)) // no token at all
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token must 401, got %d", resp.StatusCode)
	}
}

func TestBearerHeaderAccepted(t *testing.T) {
	addr, _ := startTestWS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://%s/events", addr), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer sekrit"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn.CloseNow()
}

func TestShutdownOnCtxCancel(t *testing.T) {
	b := bus.New()
	ctx, cancel := context.WithCancel(context.Background())
	addr, err := Start(ctx, 0, "tok", b)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := http.Get(fmt.Sprintf("http://%s/events", addr)); err != nil {
			return // server is down — good
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server still answering after ctx cancel")
}
```

Run: `go test ./internal/wsserver/ -v` → FAIL

- [ ] **Step 3: Implement**

```go
// Package wsserver streams live events over a token-gated WebSocket.
// Loopback-only by construction (spec §9): the bind address is
// hardcoded to 127.0.0.1 and not configurable. Bus semantics apply —
// a slow consumer misses events; durability lives in the store.
package wsserver

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/tufantunc/RuntimePulse/internal/bus"
)

// Start listens on 127.0.0.1:port (0 = ephemeral) and serves /events.
// Returns the actual listen address; shuts down when ctx is cancelled.
func Start(ctx context.Context, port int, token string, b *bus.Bus) (string, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("token")
		if got == "" {
			got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return // Accept already wrote the HTTP error
		}
		defer conn.CloseNow()
		ch, cancel := b.Subscribe(64)
		defer cancel()
		for {
			select {
			case <-r.Context().Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if err := wsjson.Write(r.Context(), conn, ev); err != nil {
					return
				}
			}
		}
	})
	srv := &http.Server{Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	go srv.Serve(ln)
	return ln.Addr().String(), nil
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test -race -count=2 ./internal/wsserver/ -v && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/wsserver/
git commit -m "feat(ws): loopback-only token-gated websocket event stream"
```

---

### Task 4: Daemon wiring (`--ws-port`, token file) + smoke + docs

**Files:**
- Modify: `internal/daemon/daemon.go`, `cmd/runtimepulse/cmd_daemon.go`, `scripts/smoke.sh`, `docs/PROJECT.md`
- Test: `internal/daemon/ws_test.go`

- [ ] **Step 1: Write the failing test**

```go
package daemon

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeWSTokenFileAndAuth(t *testing.T) {
	dir := shortTempDir(t)
	d, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); d.Close() })
	go d.Serve(ctx)

	addr, err := d.ServeWS(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	tokenBytes, err := os.ReadFile(filepath.Join(dir, "ws-token"))
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if len(token) < 16 {
		t.Fatalf("token too short: %q", token)
	}
	info, _ := os.Stat(filepath.Join(dir, "ws-token"))
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token file perm = %o, want 0600", perm)
	}

	resp, err := http.Get("http://" + addr + "/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token must 401, got %d", resp.StatusCode)
	}
	// with the right token but no websocket upgrade: not 401 (auth passed)
	resp, err = http.Get("http://" + addr + "/events?token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("valid token must pass auth")
	}

	// token persists across calls
	d2addr, err := d.ServeWS(ctx, 0)
	_ = d2addr
	if err == nil {
		t.Log("second ServeWS allowed (separate listener) — acceptable")
	}
	tokenBytes2, _ := os.ReadFile(filepath.Join(dir, "ws-token"))
	if string(tokenBytes) != string(tokenBytes2) {
		t.Fatal("token must persist, not regenerate")
	}
}
```

Run: `go test ./internal/daemon/ -run TestServeWS -v` → FAIL

- [ ] **Step 2: Implement ServeWS**

In `internal/daemon/daemon.go` (imports: `crypto/rand`, `encoding/hex`, `github.com/tufantunc/RuntimePulse/internal/wsserver`):

```go
// ServeWS starts the optional WebSocket event stream (spec §9: opt-in,
// loopback-only, token-gated). The token persists in <dir>/ws-token
// (0600) so external consumers can read it across daemon restarts.
func (d *Daemon) ServeWS(ctx context.Context, port int) (string, error) {
	token, err := loadOrCreateToken(filepath.Join(d.Dir, "ws-token"))
	if err != nil {
		return "", err
	}
	addr, err := wsserver.Start(ctx, port, token, d.bus)
	if err != nil {
		return "", err
	}
	log.Printf("ws: streaming events on ws://%s/events (token: %s/ws-token)", addr, d.Dir)
	return addr, nil
}

func loadOrCreateToken(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(data)); tok != "" {
			return tok, nil
		}
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}
```

(add `strings` import if missing)

- [ ] **Step 3: CLI flag**

In `cmd/runtimepulse/cmd_daemon.go`, add a `--ws-port` int flag (default 0 = disabled). After `daemon.New` succeeds and the signal ctx exists, before `d.Serve(ctx)`:

```go
			if wsPort > 0 {
				if _, err := d.ServeWS(ctx, wsPort); err != nil {
					return fmt.Errorf("starting ws server: %w", err)
				}
			}
```

- [ ] **Step 4: Smoke — ws auth check**

The smoke restarts the daemon mid-script; change the SECOND daemon start to include ws and assert auth (using a port derived from the script's PID to avoid collisions):

```bash
ws_port=$((20000 + $$ % 20000))
"$rp" daemon --ws-port "$ws_port" >"$dir/daemon-out2.log" 2>&1 &
dpid=$!
sleep 0.5
```

and after the restart re-arm check:

```bash
# --- ws: opt-in stream is loopback + token gated ---
ws_code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$ws_port/events")
[ "$ws_code" = "401" ] || { echo "FAIL: ws without token must 401, got $ws_code"; exit 1; }
ws_token=$(cat "$dir/ws-token")
ws_code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$ws_port/events?token=$ws_token")
[ "$ws_code" != "401" ] || { echo "FAIL: ws with token must pass auth"; exit 1; }
```

- [ ] **Step 5: Docs**

`docs/PROJECT.md`:
- §7.1 CLI block: add `runtimepulse daemon --ws-port 8787   # opt-in event stream (127.0.0.1, token in ~/.runtimepulse/ws-token)`.
- §7.3: document the optional `repo:` field (defaults to apply's cwd) and the rollback-on-partial-failure behavior.
- §13 item 5: mark workflow YAML + WebSocket done; note "Claude Channels optimization deferred while the feature is a research preview".

- [ ] **Step 6: Full verification**

Run: `go test -race ./... && go vet ./... && gofmt -l .` then `./scripts/smoke.sh` ×3 → all `SMOKE OK`.

- [ ] **Step 7: Commit**

```bash
git add internal/daemon/ cmd/ scripts/smoke.sh docs/PROJECT.md
git commit -m "feat(daemon): opt-in websocket event stream with persistent token"
```

---

## Self-Review Notes

- **Spec coverage:** §7.3 workflow→rules with chaining → Tasks 1–2; "no workflow entity in core" honored (pure compile, client-side apply); §9 WS posture (opt-in, 127.0.0.1 hardcoded, token, 0600) → Tasks 3–4; §13 item 5 complete minus the explicitly deferred Channels item.
- **Type consistency:** `workflow.Parse/Rules` produce `core.Rule` consumed by the existing `rule.add` param shape in apply; `wsserver.Start(ctx, port, token, *bus.Bus)` matches `daemon.ServeWS`'s call; `d.bus` field already exists in Daemon.
- **Stage lessons honored:** smoke sections use `has`/`count`, never `| grep -q`; the bad-workflow rollback check pins the partial-failure contract; ws test asserts token file perms 0600 (mirrors the socket-perm test pattern).
- **Known limits (documented, deliberate):** apply rollback is best-effort (a crash mid-apply can still leak rules — the user can `rule list`/`rm`); ws stream has no replay/backfill (bus semantics; reconcile via `get_events`).
```
