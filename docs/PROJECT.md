# RuntimePulse

**Event-driven session continuation engine for AI coding agents**

RuntimePulse is an orchestration system that lets AI coding agents (Claude Code, Cursor CLI, Codex CLI, OpenCode) continue their workflows interrupt-free based on runtime environment events (process, docker, http, file, build, git).

> Core idea: runtime events do not "notify agents" — they **resume agent sessions with context injection.**

RuntimePulse is not a messaging system, a push-notification layer, or an agent chat platform. It is an event-driven runtime engine, a session continuation orchestrator, and a cross-agent workflow coordinator.

---

## 1. Core Problem

AI coding agents struggle with runtime uncertainty:

* "Is the service ready?"
* "Did the build finish?"
* "Is the database healthy?"
* "Can I proceed to the next step?"
* "Should I retry or wait?"

Current workarounds — polling, manual sleeps, brittle scripts, CI step chaining — waste tokens, block agent turns, and fail silently.

## 2. Solution

RuntimePulse converts environment state changes into session continuations:

```
Event → Rule match → Continuation → Agent resume (prompt injection)
```

No agent platform supports pushing messages into a running session. RuntimePulse therefore never pushes: when a condition is met, it **resumes the correct session via the agent's own CLI and injects the event as a new prompt turn**. The agent ends its turn voluntarily ("wake me when Postgres is healthy"), and RuntimePulse wakes it.

---

## 3. Process Architecture

RuntimePulse is **daemon-first**. A single long-running daemon owns all state and all watchers; the CLI and the MCP server are thin clients.

```
┌─────────────────────────── runtimepulsed (daemon) ───────────────────────────┐
│                                                                              │
│  Watchers (http / docker / file / process / build / git)                     │
│        │ state transitions                                                   │
│        ▼                                                                     │
│  Event Bus ──► Event Store (SQLite, WAL mode)                                │
│        │                                                                     │
│        ▼                                                                     │
│  Rule Engine ──► Continuation Dispatcher ──► per-session FIFO queues         │
│                          │                                                   │
│                          ▼                                                   │
│                  Agent Adapters (claude / cursor / codex / opencode)         │
│                          │ supervised execution                              │
│                          └──► continuation.completed / .failed ─► Event Bus ◄┐
└──────────────────────────────────────────────────────────────────────────────┘
        ▲ unix socket (mode 0600), JSON-RPC
        │
   CLI (runtimepulse …)          MCP server (stdio, runs inside agent sessions)
```

* The daemon is the single source of truth. Watches survive terminal closure.
* If the daemon is not running, the first CLI/MCP command auto-starts it.
* Continuation results are fed back into the event bus as events — this loop is what enables multi-step workflow chaining without a separate workflow engine.

## 4. Core Concepts

Five first-class entities.

### 4.1 Event

An immutable fact about the environment.

```json
{
  "id": "evt-001",
  "type": "docker.healthy",
  "source": "postgres",
  "payload": { "container": "postgres", "health": "healthy" },
  "timestamp": "2026-06-11T10:00:00Z"
}
```

**Event taxonomy (both directions are first-class):**

| Watch type | Positive | Negative |
|---|---|---|
| http | `http.available` (2xx/3xx) | `http.unavailable` |
| tcp | `tcp.available` | `tcp.unavailable` |
| docker | `docker.healthy`, `docker.started` | `docker.unhealthy`, `docker.stopped` |
| file | `file.created`, `file.changed` | `file.removed` |
| process | `process.started` | `process.exited` |
| exec wrapper | `exec.succeeded` | `exec.failed` |
| git | `git.branch.changed`, `git.ref.changed` | — |
| (internal) | `continuation.completed` | `continuation.failed` |

Notes: `tcp` is the service-agnostic readiness check (e.g. postgres on `localhost:5432`). Build events come from the `runtimepulse exec --label <name> -- <cmd>` wrapper, which runs the command and emits `exec.succeeded/failed` with the label as `source` — this also covers `docker compose up --wait` style readiness. `git.pushed` was dropped: a push is not reliably detectable from the local machine; `git.ref.changed` (refs) and `git.branch.changed` (HEAD) are. Docker watching uses `docker events` / `docker inspect` CLI subprocesses (push-based, no SDK dependency).

Failure events are not an afterthought: the moments an agent most needs to be woken are the failures.

### 4.2 Watch

A runtime condition observer, hosted by the daemon.

```json
{
  "id": "watch-001",
  "type": "http",
  "target": "http://localhost:3000",
  "config": { "interval": "2s", "stabilityThreshold": "5s" }
}
```

**Triggering semantics — initial check + edge:**

1. When a watch is created, the current state is measured immediately and emitted as an event (if the condition already holds, the positive event fires right away). This makes the "service was already ready" deadlock impossible.
2. After that, only state *transitions* emit events.
3. Flapping protection: a state must hold for `stabilityThreshold` before its transition event is emitted.

### 4.3 Rule

The binding between events and actions. Watches, sessions, and rules live independently; rules connect them.

```json
{
  "id": "rule-001",
  "eventSelector": { "type": "docker.healthy", "source": "postgres" },
  "action": {
    "kind": "continue_session",
    "sessionId": "abc123",
    "promptTemplate": "{{.Event.Source}} is healthy. Continue the migration step."
  },
  "oneShot": true,
  "expiresAt": "2026-06-11T12:00:00Z"
}
```

* One event may trigger many rules (fan-out); one session may be bound by many rules.
* `eventSelector` matches on `type` and optionally `source`.
* Prompt templates are Go `text/template`; only machine-generated event fields are available in template context. Free-form external text never flows into a template.
* `action.kind` is extensible (future kinds: `run_command`, `emit_event`).

### 4.4 Session

An agent execution context known to the registry.

```json
{
  "sessionId": "abc123",
  "agent": "claude",
  "repoPath": "/Users/me/work/project-x",
  "state": "waiting"
}
```

**State machine:** `waiting → queued → resuming → running → waiting | done`

**Registration paths (two, no auto-discovery):**

1. **MCP self-registration (primary).** While running, an agent calls the `create_rule` MCP tool: *"I am Claude session X in repo Y; when `docker.healthy` fires for `postgres`, resume me with this prompt."* One call registers the session and creates the rule. The agent then ends its turn; the session is now `waiting`.
2. **Manual CLI registration.** `runtimepulse session register --agent cursor --session abc123 --repo ~/work/x` — for external orchestration and agents that cannot speak MCP.

### 4.5 Continuation

The key abstraction: a resumed execution with injected context. **Not a message push.**

```json
{
  "id": "cont-001",
  "ruleId": "rule-001",
  "eventId": "evt-001",
  "sessionId": "abc123",
  "command": "claude -p --resume abc123 \"postgres is healthy. Continue the migration step.\"",
  "state": "completed",
  "exitCode": 0,
  "outputSummary": "Migrations applied; 12 tables created."
}
```

`(ruleId, eventId)` is UNIQUE — the same event can never trigger the same rule twice (idempotency).

`continuation.completed` / `continuation.failed` events carry the originating rule's label (or id, when no label is set) as their `source`, so downstream rules can select on it.

---

## 5. End-to-End Flow (Outbox Pattern)

```
1. Watcher detects a transition.
2. In ONE SQLite transaction: the event is stored, matching rules are evaluated,
   resulting continuation records are written as `pending`, and any oneShot
   rules are marked consumed.
3. The dispatcher picks up pending continuations and enqueues them on the
   target session's FIFO queue (one continuation runs per session at a time;
   different sessions run in parallel).
4. The adapter renders the prompt template, then executes the resume command
   in the session's repoPath, supervised.
5. The result (exit code, output summary) is written to the continuation
   record, and a `continuation.completed` or `continuation.failed` event is
   emitted onto the bus — which may match further rules (chaining).
6. On daemon restart, `pending`/`running` continuations are recovered from the
   store and re-dispatched.
```

**Delivery guarantee: at-least-once, made safe by idempotency.** Best-effort delivery would reintroduce the exact trust problem ("was I supposed to be woken?") this product exists to remove.

**Concurrency:** per-session FIFO queue. Agent CLIs do not support parallel resumes of the same session; queueing preserves every event in order.

## 6. Agent Adapters

Each adapter implements a common interface:

```go
type Adapter interface {
    Resume(session Session, prompt string) (Result, error)
    Validate() error // CLI installed? Flags valid for this version? (smoke test)
}
```

| Agent | Resume command | Notes |
|---|---|---|
| Claude Code | `claude --resume <id> -p "<prompt>" --output-format json` | Concurrent resumes of one session interleave — FIFO queue is mandatory. Channels (research preview) can push into *running* sessions; CLI resume is the fallback. |
| Cursor CLI | `agent -p --resume <id> --output-format json --workspace <repo> "<prompt>"` | Binary renamed `cursor-agent` → `agent`; detect both. `-p`+`--resume` combination unconfirmed in official docs — `Validate()` must smoke-test it. |
| Codex CLI | `codex exec resume <id> --json -C <repo> "<prompt>"` | Needs `--sandbox workspace-write` for edits. No session-list command; ids discovered from `~/.codex/sessions/`. |
| OpenCode | `opencode run --session <id> --dir <repo> --format json "<prompt>"` | Exit codes unreliable (documented exit-0-on-error history) — parse JSON events. Prefer `opencode serve` + `POST /session/:id/message` when a server runs. |

v1 passes no permission flags on resume; the agent runs with its project-configured permissions (owner decision, 2026-06-11).

Adapter binaries are overridable via `RUNTIMEPULSE_CLAUDE_BIN` / `RUNTIMEPULSE_CURSOR_BIN` / `RUNTIMEPULSE_CODEX_BIN` / `RUNTIMEPULSE_OPENCODE_BIN` (hermetic tests). All prompts pass positionally after a `--` terminator. v1 passes no permission/sandbox flags for any agent.

Per-CLI details (session id locations, output schemas, auth, MCP registration) are distilled in [`docs/reference/`](reference/README.md), verified 2026-06-11. These surfaces drift; `Validate()` exists precisely because of that.

Future targets: LangGraph agents, AutoGen, custom orchestrators.

## 7. Interfaces

### 7.1 CLI

```bash
runtimepulse daemon                 # run the daemon in foreground (also auto-started)
runtimepulse daemon --ws-port 8787   # opt-in event stream (127.0.0.1, token in ~/.runtimepulse/ws-token)
runtimepulse status                 # daemon + watch + session overview

runtimepulse watch add http --url http://localhost:3000
runtimepulse watch add tcp --addr localhost:5432
runtimepulse watch add docker --container postgres
runtimepulse watch add file --path dist/index.js
runtimepulse watch add process --pattern "vite"
runtimepulse watch add git --repo .
runtimepulse watch list | rm <id>

runtimepulse exec --label build -- npm run build   # emits exec.succeeded/failed

runtimepulse rule add --on docker.healthy:postgres \
  --session abc123 --agent claude \
  --prompt "Postgres is ready. Continue." --one-shot
runtimepulse rule list | rm <id>

runtimepulse session register --agent cursor --session abc123 --repo ~/work/x
runtimepulse session list

runtimepulse wait http http://localhost:3000   # blocking client (scripts/CI)
runtimepulse events --follow                   # stream events as JSON lines
runtimepulse apply workflow.yaml               # compile workflow file to rules
runtimepulse continue --session abc123 --agent claude --prompt "…"  # manual trigger
runtimepulse setup                             # detect installed agent CLIs, register the MCP server
```

Event output is JSON lines:

```json
{"type":"http.available","source":"localhost:3000"}
{"type":"docker.healthy","source":"postgres"}
```

### 7.2 MCP Tools

MCP is a tool interface on top of the daemon — not the core system.

| Tool | Purpose |
|---|---|
| `create_watch` | Create a watch (idempotent if an equivalent watch exists). |
| `create_rule` | Register the calling session and bind it to an event — the primary "wake me when X" call. |
| `cancel_rule` | Cancel a pending rule. |
| `wait_for_event` | Block inside the current turn until an event arrives (with timeout). |
| `get_events` | Query event history. |

**Two waiting modes, made explicit:**

* **Wait in-turn** (`wait_for_event`): the agent's turn stays open and blocks. Costs context/timeout budget. Use for short waits.
* **Release-and-resume** (`create_rule` + end turn): the agent ends its turn; RuntimePulse resumes it when the event fires. **Recommended** — this is the product's core flow.

Agents register the server with: `claude mcp add --transport stdio runtimepulse -- runtimepulse mcp`. `wait_for_event` is live-only (blocks for an event published after the call); `create_rule` is the primary release-and-resume flow.

### 7.3 Workflow Files

Multi-step chains are declared in YAML and compiled to rules by `runtimepulse apply`. There is no workflow entity in the core — only syntax sugar that emits rules. Steps chain through `continuation.completed` events.

```yaml
# workflow.yaml
session: abc123
agent: claude
repo: /path/to/repo   # optional: session's repoPath; defaults to cwd of `apply`
steps:
  - label: step-1
    on: { type: docker.healthy, source: postgres }
    prompt: "Postgres is ready. Run the migrations."
  - label: step-2
    on: { type: continuation.completed, source: step-1 }
    prompt: "Migrations done. Start the server and run the tests."
  - label: step-3
    on: { type: continuation.completed, source: step-2 }
    prompt: "Tests finished. Report the results."
```

`runtimepulse apply workflow.yaml` compiles all steps to oneShot rules in order. On partial failure (e.g. a bad prompt template on step 3 of 5), the already-created rules are removed best-effort and the command errors — a crashed apply can leak rules; use `rule list` and `rule remove` to inspect.

## 8. Storage

Single SQLite database (WAL mode), CGO-free driver (`modernc.org/sqlite`). Tables: `events`, `watches`, `rules`, `sessions`, `continuations`. A retention sweep runs hourly in the daemon (and at boot): idle sessions are removed after 7 days — never while running or referenced by an active rule — terminal continuations after 30 days, events after 30 days. The unix socket and database live under `~/.runtimepulse/`.

## 9. Security Posture

Local, single-user developer machine by assumption (documented, not accidental):

* The daemon listens **only** on a unix socket with mode 0600. No TCP port by default.
* The opt-in WebSocket event stream (`--ws-port`) binds to `127.0.0.1` only and requires a token (stored in `~/.runtimepulse/ws-token`, mode 0600).
* Prompt templates are authored by the rule creator; template context exposes only machine-generated event fields. RuntimePulse never forwards arbitrary external text into an agent prompt.
* Continuations execute agent CLIs as the daemon's own user; RuntimePulse adds no privilege boundary.
* On Windows, the socket file does not carry a 0600 mode (POSIX permissions do not apply); access control relies on the user-private ACLs inherited by `%USERPROFILE%\.runtimepulse\`, which are accessible only to the owning user by default.

## 10. Technology Stack

| Concern | Choice |
|---|---|
| Language | Go |
| CLI framework | cobra |
| Storage | SQLite via `modernc.org/sqlite` (CGO-free, easy cross-compile) |
| File/git watching | fsnotify |
| Docker watching | `docker events` / `docker inspect` CLI subprocesses (push-based, no SDK) |
| Process watching | gopsutil/v4 (command-line substring match; identical semantics on every platform) |
| HTTP/TCP watching | `net/http` / `net.Dial` probes (internal polling with stability threshold) |
| Prompt templates | Go `text/template` |
| IPC | unix socket, JSON-RPC |

"Zero polling" means the **agent** never polls. Watchers use push APIs where they exist (docker events, fsnotify) and efficient internal probing where they don't (http).

## 11. Testing Strategy

* **Rule engine + dispatcher:** pure unit tests (no I/O).
* **Watchers:** integration tests against disposable targets (httptest server, temp files, throwaway containers).
* **Adapters:** `Validate()` smoke tests plus golden tests over recorded CLI output.
* **End-to-end:** the acceptance scenario below, driven by docker compose.

## 12. Acceptance Scenario

The full workflow must run with zero agent-side polling:

```
docker compose up
↓ docker.healthy (postgres)
RuntimePulse resumes session → agent runs migrations
↓ continuation.completed
RuntimePulse resumes session → agent starts server, runs tests
↓ continuation.completed
RuntimePulse resumes session → agent reports results
```

## 13. Implementation Order

Build order toward the final architecture (each stage lands fully designed, not as a throwaway MVP):

1. ✅ **Core:** SQLite store, event bus, rule engine, daemon skeleton + unix socket RPC.
2. ✅ **Watchers:** http, docker, file, process, build, git — with initial-check + edge semantics.
3. ✅ **Continuation:** dispatcher, per-session queues, supervision, Claude Code adapter first, ✅ Cursor/Codex/OpenCode adapters (all four share one supervised CLI runner; registry wired in daemon).
4. **Interfaces:** ✅ full CLI surface (stages 1–3), ✅ MCP server and tools (`create_watch`, `create_rule`, `wait_for_event`, `get_events`, `cancel_rule`) over stdio.
5. ✅ **Layers:** ✅ workflow YAML compiler (`runtimepulse apply`, `internal/workflow`), ✅ WebSocket streaming (127.0.0.1 + token, `internal/wsserver`, `daemon --ws-port`). Claude Channels optimization deferred while the feature is a research preview.

## 14. Design Decisions

The rationale behind every decision in this document is recorded in
[`docs/superpowers/specs/2026-06-11-runtimepulse-architecture-design.md`](superpowers/specs/2026-06-11-runtimepulse-architecture-design.md).
