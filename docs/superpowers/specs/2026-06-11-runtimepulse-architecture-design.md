# RuntimePulse Architecture — Design Decisions

**Date:** 2026-06-11
**Status:** Approved
**Scope:** Resolves every open question in the original spec. The resulting architecture is written into `docs/PROJECT.md`, which remains the authoritative spec. This document records the *decisions and their rationale*.

A deliberate framing choice applies throughout: **we design for the final version, not an MVP.** Implementation proceeds in stages (PROJECT.md §13), but no stage ships a throwaway design.

---

## Decision 1 — Process model: daemon-first

**Decision:** A single long-running daemon (`runtimepulsed`) owns watchers, event bus, SQLite store, rule engine, and dispatcher. CLI and MCP server are thin clients over a unix socket (JSON-RPC). The daemon auto-starts on first client command.

**Why:** Watchers must outlive terminals; supervision, rule chaining, and (later) WebSocket streaming all need a resident process that owns shared state. Alternatives rejected: *process-per-command* (no common brain — chaining and supervision impossible), *hybrid optional daemon* (two code paths, double the test surface, divergent behavior).

## Decision 2 — Event→Session binding: Rule as a first-class entity

**Decision:** A `Rule` independently binds an `eventSelector` to an `action` (`continue_session` + prompt template), with `oneShot` and `expiresAt`. Watches, sessions, and rules live independently.

**Why:** This was the original spec's biggest gap — "resolve target session" had no defined mechanism. A first-class Rule gives fan-out (one event → many sessions), multi-condition bindings (one session ← many rules), and an extensible `action.kind` for future orchestration. Rejected: *subscription embedded in session* (single condition, no fan-out), *YAML-only rules* (blocks agents from creating rules dynamically at runtime via MCP).

## Decision 3 — Session registration: MCP self-registration + manual CLI; no auto-discovery

**Decision:** Two registration paths. Primary: the agent registers itself via the `create_rule` MCP tool (one call = registration + rule), then ends its turn. Secondary: `runtimepulse session register` for external orchestration and non-MCP agents. **Auto-discovery of agent session stores was considered and explicitly excluded.**

**Why:** Self-registration is the natural shape of "wake me when X" — no human in the loop. Manual CLI covers scripts/CI. Auto-discovery (scanning `~/.claude/projects/` etc.) adds a fragile, agent-version-coupled surface for a convenience feature; cut by owner decision.

## Decision 4 — Continuations are fully supervised; per-session FIFO queues

**Decision:** The daemon spawns the resume command itself, captures exit code and an output summary, records the result, and emits `continuation.completed` / `continuation.failed` events onto the bus. Per session, exactly one continuation runs at a time; concurrent events queue FIFO. Different sessions run in parallel.

**Why:** Feeding continuation results back as events makes multi-step chaining (the acceptance scenario) an emergent property of the Rule mechanism — no separate workflow engine. Retry-on-failure becomes "a rule matching `continuation.failed`". Fire-and-forget was rejected: it makes the acceptance scenario impossible. FIFO chosen over drop (data loss) and coalesce (added complexity; can be revisited as a rule option later). Agent CLIs cannot parallel-resume one session anyway.

## Decision 5 — Watcher semantics: initial check + edge; failure events first-class

**Decision:** On watch creation, current state is measured and emitted immediately; afterwards only transitions emit. Every watch type emits both positive and negative events (`http.unavailable`, `docker.unhealthy`, `build.failed`, `process.exited`, …). Flapping is suppressed by a configurable `stabilityThreshold` (state must hold before its transition emits).

**Why:** Pure edge triggering has a classic deadlock — the condition became true *before* the rule existed, so no event ever fires. Initial-check removes the race without re-emitting steady state. Failure events: the spec's own motivating questions ("should I retry or wait?") are failure scenarios; an engine that can only announce good news cannot answer them.

## Decision 6 — Delivery: at-least-once with idempotency (outbox pattern)

**Decision:** Event insert, rule evaluation, continuation creation, and oneShot consumption happen in one SQLite (WAL) transaction. The dispatcher reads pending work from the store. On restart, pending/running continuations are recovered. `(ruleId, eventId)` is UNIQUE on continuations — duplicate dispatch is structurally impossible. Event retention is configurable (default 30 days / size cap).

**Why:** "I was supposed to be woken and wasn't" is precisely the trust failure this product eliminates; best-effort delivery would reintroduce it. Exactly-once doesn't exist in practice; at-least-once + idempotency is the standard correct answer.

## Decision 7 — Security: local-only by default

**Decision:** Unix socket with mode 0600; no TCP listener by default. Future WebSocket streaming binds `127.0.0.1` and requires a token. Prompt template context exposes only machine-generated event fields — arbitrary external text never flows into an agent prompt. Single-user developer machine is a documented assumption.

**Why:** The system executes CLIs and injects text into agent prompts — a real surface. Constraining transport (socket perms) and template inputs (no free-form text) bounds it without building an auth system the local use case doesn't need. Network-ready-from-day-one was rejected as premature surface area.

## Decision 8 — Workflow YAML: a compile-to-rules layer

**Decision:** `runtimepulse apply workflow.yaml` compiles a declarative step list into a set of rules chained via `continuation.completed` events. The core has no workflow entity.

**Why:** Five-step chains are painful to assemble rule-by-rule, but a second engine would duplicate the rule mechanism. Compiling YAML→rules keeps one execution model and makes workflows inspectable (`rule list` shows exactly what will run).

## Decision 9 — Documentation language: English

**Decision:** All project documentation is written in English (the original spec was mixed Turkish/English).

**Why:** Consistency with code, commits, and CLI output; ready for potential open-sourcing and outside contributors.

## Implementation-level choices (made by recommendation, low controversy)

| Concern | Choice | Note |
|---|---|---|
| SQLite driver | `modernc.org/sqlite` | CGO-free; trivial cross-compilation |
| CLI framework | cobra | de facto standard in Go |
| Prompt templates | Go `text/template` | stdlib, no dependency |
| File watching | fsnotify | push-based |
| Docker watching | Engine Events API | push-based |
| HTTP watching | `net/http` probes | internal polling + stability threshold; "zero polling" refers to the *agent* never polling |
| IPC | unix socket + JSON-RPC | matches Decision 7 |
| State dir | `~/.runtimepulse/` | socket + database |

## Open items deferred to implementation

* Exact resume flags per agent CLI must be verified against installed versions (`Adapter.Validate()` exists for this). The commands in PROJECT.md §6 are best-known-current, not guaranteed.
* "Claude Channels" as an optimization path needs validation against what Claude Code actually ships; CLI resume is the contractual fallback either way.
* Coalescing queued events into one prompt (rejected as default in Decision 4) may return later as an opt-in rule setting.
