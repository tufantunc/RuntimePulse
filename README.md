# RuntimePulse

**Event-driven session continuation engine for AI coding agents.**

RuntimePulse watches runtime conditions — a port opening, a container turning healthy, a file appearing, a build finishing — and, when a condition is met, **resumes the relevant AI agent session** (Claude Code, Cursor CLI, Codex CLI, OpenCode) with the event injected as a new prompt turn:

```
Event → Rule match → Continuation → claude --resume abc123 -p "Postgres is healthy. Continue the migration."
```

The agent never polls and is never pushed messages. It ends its turn voluntarily ("wake me when Postgres is healthy"), and RuntimePulse wakes it through the agent's own CLI. Failure events are first-class: the moments an agent most needs to be woken are `build.failed` and `docker.unhealthy`, not just the happy path.

RuntimePulse is **not** a messaging system, a push-notification layer, or a chat platform. It is a session continuation orchestrator.

## Why

AI coding agents stall on runtime uncertainty: *Is the service up? Did the build pass? Can I proceed?* The usual workarounds — polling loops, `sleep 30`, brittle scripts — waste tokens, block agent turns, and fail silently. RuntimePulse replaces all of them with one primitive: **a rule that resumes your session when reality changes.**

## How it works

A single daemon owns everything; the CLI and the MCP server are thin clients over a unix socket (0600, no TCP by default). The daemon auto-starts on the first command.

```
┌─────────────────────────── runtimepulse daemon ──────────────────────────────┐
│  Watchers (http / tcp / docker / file / process / git)                       │
│        │ state transitions                                                   │
│        ▼                                                                     │
│  Event Bus ──► Event Store (SQLite, WAL)                                     │
│        ▼                                                                     │
│  Rule Engine ──► Continuation Dispatcher ──► per-session FIFO queues         │
│                          ▼                                                   │
│                  Agent Adapters (claude / cursor / codex / opencode)         │
│                          └──► continuation.completed/failed ──► Event Bus ◄──┐
└──────────────────────────────────────────────────────────────────────────────┘
        ▲ unix socket                       ▲ stdio
   CLI (runtimepulse …)                MCP server (inside agent sessions)
```

Continuation results feed back into the bus as events, so **multi-step chains are just rules** — no separate workflow engine. Delivery is at-least-once: everything is transactional in SQLite, and a daemon restart recovers in-flight work.

## Quick taste

```bash
# Register a Claude Code session and arm a one-shot rule
runtimepulse session register --agent claude --session <session-id> --repo .
runtimepulse rule add --on tcp.available:localhost:5432 --session <session-id> \
  --prompt 'Postgres is up. Run the migrations.' --one-shot

# Watch the condition — when port 5432 opens, the session resumes itself
runtimepulse watch add tcp --addr localhost:5432
```

Or let the agent do it from inside its own session via MCP:

```bash
claude mcp add --transport stdio runtimepulse -- runtimepulse mcp
```

…after which the agent can call `create_watch` / `create_rule` ("wake me when X") and end its turn.

## Install

Runs on macOS, Linux, and Windows 10 1803+.

**Homebrew (macOS/Linux):**

```bash
brew install --cask tufantunc/tap/runtimepulse
```

(Linux: requires Homebrew ≥ 4.5.)

**Install script (macOS/Linux):**

```bash
curl -fsSL https://raw.githubusercontent.com/tufantunc/RuntimePulse/main/scripts/install.sh | sh
```

Auto-detects your platform, verifies checksums, installs to `~/.local/bin` (override with `INSTALL_DIR=`, pin with `VERSION=vX.Y.Z`).

**Manual download (all platforms, incl. Windows):** grab the archive for your OS/arch from the [releases page](https://github.com/tufantunc/RuntimePulse/releases) — Windows ships as a zip — and put `runtimepulse` on your PATH.

Then register RuntimePulse with your installed agents: `runtimepulse setup` (detects Claude/Cursor/Codex/OpenCode and wires up the MCP server).

**From source:** requires Go 1.26+ — `go build -o runtimepulse ./cmd/runtimepulse` (reports its version as `dev (<commit>)`).

Verify a checkout: `go test -race ./... && ./scripts/smoke.sh` (hermetic; must end `SMOKE OK`).

## Documentation

| Guide | What it covers |
|---|---|
| [Getting started](docs/usage/getting-started.md) | Build, daemon lifecycle, state directory, a first end-to-end run |
| [Watchers](docs/usage/watchers.md) | The six watch types, triggering semantics, the `exec` wrapper |
| [Rules, sessions & continuations](docs/usage/rules-and-sessions.md) | The core model: binding events to session resumes, chaining, prompts |
| [Workflows](docs/usage/workflows.md) | Declaring multi-step chains in YAML with `runtimepulse apply` |
| [MCP server](docs/usage/mcp.md) | Letting agents register their own wake-ups from inside a session |
| [WebSocket stream](docs/usage/websocket.md) | Opt-in live event stream for external consumers |

Design references:

* [`docs/PROJECT.md`](docs/PROJECT.md) — the authoritative spec (architecture, entities, acceptance scenario).
* [`docs/superpowers/specs/`](docs/superpowers/specs/) — the rationale behind every architectural decision.
* [`docs/reference/`](docs/reference/) — verified per-agent CLI references (resume flags, session id discovery, caveats).

## Supported agents

| Agent | Resume mechanism |
|---|---|
| Claude Code | `claude --resume <id> --output-format json -p -- "<prompt>"` |
| Cursor CLI | `agent -p --resume <id> --output-format json -- "<prompt>"` |
| Codex CLI | `codex exec resume <id> --skip-git-repo-check -- "<prompt>"` |
| OpenCode | `opencode run --session <id> -- "<prompt>"` |

All resumes run in the session's registered repo directory with **no permission/sandbox flags** — the agent operates under its own configured permissions (a deliberate v1 decision; pre-approve the tools your workflow needs in the agent's project settings).

## Status

The full spec roadmap is implemented: core engine, watchers, supervised dispatcher with chaining, MCP server, all four adapters, workflow YAML compiler, and the opt-in WebSocket stream. Claude Channels integration is deferred while that feature is a research preview.

## License

See [LICENSE](LICENSE).
