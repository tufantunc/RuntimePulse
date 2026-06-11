# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Status

Stages 1–2 implemented and merged to main: core engine (store/bus/engine/daemon/client/CLI) and watchers (http, tcp, file, process, docker, git + `exec` wrapper). Next per spec §13: continuation dispatcher + agent adapters, then MCP server, then workflow YAML/WebSocket.

Commands: `go build ./...`, `go test -race ./...`, `go test -run TestName ./internal/...`, `golangci-lint run`, end-to-end: `./scripts/smoke.sh` (hermetic, must end `SMOKE OK`). Package layout: `internal/core` (domain types), `internal/store` (SQLite, single-connection — NEVER touch `s.db` while a tx is open), `internal/bus`, `internal/engine`, `internal/watch`, `internal/daemon`, `internal/client`, `cmd/runtimepulse`. Continuations currently stop at `pending` (dispatcher is the next stage). Prompts are rendered at ingest and stored on the continuation — the dispatcher must NOT re-render.

Two authoritative documents — read both before designing or implementing anything:

* `docs/PROJECT.md` — the spec: architecture, entities, interfaces, acceptance scenario.
* `docs/superpowers/specs/2026-06-11-runtimepulse-architecture-design.md` — the *rationale* behind every decision. Do not re-litigate a decision recorded there without flagging it to the user.

## What RuntimePulse Is

An event-driven **session continuation engine** for AI coding agents (Claude Code, Cursor CLI, Codex CLI, OpenCode). It watches runtime conditions (http, docker, file, process, build, git) and, when a condition is met, *resumes* the relevant agent session with the event injected as a new prompt turn — e.g. `claude -p --resume abc123 "Postgres is healthy. Continue the migration step."`

Key constraint: agents are **never pushed messages**. The flow is always:

```
Event → Rule match → Continuation (queued per session) → Resume agent via CLI → Inject prompt
```

It is explicitly NOT a messaging system, push-notification layer, or chat platform. "Zero polling" means the *agent* never polls; watchers may probe internally.

## Architecture (decided, not provisional)

**Daemon-first:** a single `runtimepulsed` daemon owns watchers, event bus, SQLite store (WAL, `modernc.org/sqlite`), rule engine, and continuation dispatcher. CLI and MCP server are thin clients over a unix socket (0600, JSON-RPC, no TCP). Daemon auto-starts on first client command. State lives in `~/.runtimepulse/`.

**Five core entities:** Event (bidirectional taxonomy — failure events like `build.failed` are first-class), Watch (initial-check + edge triggering, stability threshold against flapping), **Rule** (the binding: eventSelector → action with prompt template, oneShot/expiresAt), Session (state machine `waiting → queued → resuming → running`; registered via MCP self-registration or manual CLI — no auto-discovery), Continuation (supervised execution; `(ruleId, eventId)` UNIQUE for idempotency).

**Load-bearing design points:**

* Outbox pattern: event insert + rule eval + continuation creation + oneShot consumption in one SQLite transaction; at-least-once delivery with recovery on daemon restart.
* Continuation results are emitted back onto the bus as `continuation.completed/failed` events — multi-step chaining is an emergent property of rules, NOT a separate workflow engine. `runtimepulse apply workflow.yaml` merely compiles YAML to rules.
* Per-session FIFO continuation queue; different sessions run in parallel.
* Adapters implement `Resume(session, prompt)` + `Validate()`; real CLI flags must be verified per installed version (they drift).
* Prompt templates are Go `text/template`; only machine-generated event fields enter template context.

## Implementation Order (from spec §13)

1. Core: store, bus, rule engine, daemon + socket RPC
2. Watchers (http, docker, file, process, build, git)
3. Dispatcher + supervision + adapters (Claude Code first)
4. CLI surface + MCP server (`create_watch`, `create_rule`, `wait_for_event`, `get_events`, `cancel_rule`)
5. Workflow YAML compiler, WebSocket streaming (127.0.0.1 + token), Claude Channels optimization

Acceptance scenario: docker up → postgres healthy → resume session → migrations → server → tests → report, with zero agent-side polling.

## Conventions

* Stack: cobra (CLI), `modernc.org/sqlite` (no CGO), fsnotify, Docker Engine Events API.
* All documentation in English.
