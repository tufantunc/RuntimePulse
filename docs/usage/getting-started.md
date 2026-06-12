# Getting Started

## Install

Three channels are available — Homebrew, the install script, and manual download. See the [README](../../README.md#install) for all options.

**From source** (contributors and Go developers):

Requires Go 1.26+.

```bash
go build -o runtimepulse ./cmd/runtimepulse
```

Put the binary on your PATH. There is one binary for everything: the daemon, the CLI, and the MCP server are all `runtimepulse` subcommands.

## The daemon

A single daemon owns all state: watchers, the event store (SQLite), rules, sessions, and the continuation dispatcher. You normally never start it yourself — **any CLI command auto-starts it** (detached, logging to `~/.runtimepulse/daemon.log`).

```bash
runtimepulse status
# {"lastEvent":"","pendingContinuations":0,"rules":0,"runningContinuations":0,"sessions":0,"version":"0.1.0-dev","watches":0}
```

To run it in the foreground instead (e.g. under a process manager):

```bash
runtimepulse daemon                  # foreground, Ctrl-C to stop
runtimepulse daemon --ws-port 8787   # additionally serve the WebSocket event stream
```

Only one daemon runs per state directory (enforced by a lock file). Graceful shutdown leaves in-flight agent resumes marked `running`; the next daemon start re-queues them — at-least-once delivery, never a lost wake-up.

**Windows notes:** On Windows, a crashed daemon's lock file can be released with a small OS delay — an immediate restart may transiently report "already running"; retrying resolves it. The auto-started daemon is detached with no console window and never receives Ctrl-C; shutdown is effectively a process kill followed by boot recovery (in-flight resumes re-queue on the next start).

## State directory

Everything lives in `~/.runtimepulse/` (macOS/Linux) or `%USERPROFILE%\.runtimepulse\` (Windows):

| File | Purpose |
|---|---|
| `daemon.sock` | unix socket (mode 0600) — the only way in; no TCP by default |
| `runtimepulse.db` | SQLite store (events, watches, rules, sessions, continuations) |
| `daemon.log` | log of the auto-started daemon |
| `daemon.lock` | single-instance lock |
| `ws-token` | WebSocket auth token (only created when `--ws-port` is used) |

On Windows the socket file does not carry a 0600 mode (the POSIX permission model does not apply). Access control relies instead on the user-private ACLs inherited from `%USERPROFILE%` — only the owning user can reach the socket.

Override the location with `RUNTIMEPULSE_DIR=/path` — handy for isolated experiments:

```bash
RUNTIMEPULSE_DIR=$(mktemp -d) runtimepulse status   # throwaway sandbox
```

## A first end-to-end run

This walks the whole pipeline — watch → event → rule → resumed agent session — using a real Claude Code session.

**1. Find your session id.** Inside a Claude Code session, the id is a UUID (also visible in `~/.claude/projects/<project>/`). For other agents see [`docs/reference/`](../reference/).

**2. Register the session** (which agent, which repo to resume in):

```bash
runtimepulse session register --agent claude --session <session-id> --repo ~/work/myapp
```

**3. Arm a rule** — "when this event fires, resume that session with this prompt":

```bash
runtimepulse rule add --on tcp.available:localhost:5432 --session <session-id> \
  --prompt 'Postgres is accepting connections. Continue the migration step.' \
  --one-shot --label db-ready
```

**4. Create the watch:**

```bash
runtimepulse watch add tcp --addr localhost:5432
```

The current state is checked immediately (so "it was already up" can never deadlock you), then only state *transitions* emit events.

**5. Make reality change** — start your database. Watch the pipeline live in another terminal:

```bash
runtimepulse events --follow
# {"type":"tcp.available","source":"localhost:5432",...}
# {"type":"continuation.completed","source":"db-ready",...}
```

The session was resumed with your prompt as a new turn, and its completion came back as an event — which could itself trigger the next rule. That feedback loop is how [multi-step chains](rules-and-sessions.md#chaining) work.

## Inspecting state

```bash
runtimepulse status                       # one-line counts
runtimepulse events --type build.failed   # query history (JSON lines, newest first)
runtimepulse watch list
runtimepulse rule list --all              # include consumed/expired rules
runtimepulse continuations --state failed # see what went wrong, with output summaries
```

All output is JSON lines — pipe into `jq` freely.

## Testing the pipeline without real conditions

`inject` feeds a synthetic event straight into the engine — useful for testing rules and for external producers (CI, scripts):

```bash
runtimepulse inject --type docker.healthy --source postgres --payload container=postgres
```

## Where to next

* [Watchers](watchers.md) — all six watch types and the `exec` wrapper.
* [Rules, sessions & continuations](rules-and-sessions.md) — the core model in depth.
* [MCP server](mcp.md) — let the agent arm its own wake-ups; no manual session ids.
