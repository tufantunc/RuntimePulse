# MCP Server

The MCP server is how an agent uses RuntimePulse **from inside its own session** — the primary flow. Instead of a human registering sessions and rules, the agent says *"wake me when X"* itself, then ends its turn. RuntimePulse resumes it when X happens.

The MCP server is a thin client: every tool calls the same daemon the CLI talks to (and auto-starts it if needed). It runs over stdio; one binary, no extra install.

## Quick setup

```bash
runtimepulse setup
```

Detects which agent CLIs are installed (Claude Code, Cursor, Codex, OpenCode), shows a table, and registers RuntimePulse as a stdio MCP server for the ones you select (--agent skips the prompt — the flag itself is the selection). When run interactively, a numbered picker lets you choose agents by number or name (comma-separated; empty input selects all, `q` quits). Flags: `--all` registers every installed agent without prompting, `--project` registers in the current project instead of user scope, `--dry-run` shows what would happen without writing anything, `--agent <names>` targets specific agents, comma-separated (e.g. `--agent claude,opencode`). Note: Codex only supports global registration — with `--project` it falls back to user scope and says so.

The sections below are the manual per-agent reference if you prefer to register by hand.

### Installing the release-and-resume guidance

`runtimepulse setup` can also write a short rule into each agent's **global**
instruction file so the agent reaches for RuntimePulse on its own when it needs
to wait for a runtime condition (instead of polling or blocking):

- Claude → `~/.claude/CLAUDE.md`
- Codex → `~/.codex/AGENTS.md`
- opencode → `~/.config/opencode/AGENTS.md`
- Cursor → printed for you to paste into **Settings → Rules → User Rules**
  (Cursor's global rules are not file-writable from the CLI)

It is opt-in: the wizard asks before writing (default no). Non-interactively,
pass `--rules` to write or `--no-rules` to skip. The write is idempotent — a
managed, marker-delimited block refreshed in place on re-runs that never
disturbs your existing content. Combine with `--dry-run` to preview.

## Registering with your agent

**Claude Code:**

```bash
claude mcp add --transport stdio runtimepulse -- runtimepulse mcp
```

Other MCP-capable agents: configure a stdio server with command `runtimepulse` and args `["mcp"]`. Per-agent config formats are collected in [`docs/reference/`](../reference/) (`.mcp.json`, `.cursor/mcp.json`, `~/.codex/config.toml`, `opencode.json`).

## The tools

### `create_rule` — the core "wake me when X" call

Binds an event to a session resume, and **self-registers the session in the same call** when `agent` + `repoPath` are passed:

```json
{
  "eventType": "docker.healthy",
  "source": "postgres",
  "sessionId": "<the agent's own session id>",
  "agent": "claude",
  "repoPath": "/Users/me/work/app",
  "prompt": "{{.Event.Source}} is healthy. Continue the migration step.",
  "label": "db-ready",
  "oneShot": true
}
```

The intended pattern — **release-and-resume**: the agent creates the watch and the rule, tells the user "I'll continue when Postgres is up", and ends its turn. No tokens burn while waiting; RuntimePulse resumes the session as a fresh turn when the event fires.

> The agent must supply its own `sessionId` (it knows it; for Claude Code it's the session UUID). Auto-detection of the calling session is a known future improvement.

### `create_watch`

Same semantics as `watch add`: `{"type": "tcp", "target": "localhost:5432", "interval": "2s", "stability": "5s"}`. Idempotent on `(type, target)`. Initial-check + edge triggering means "is it already ready?" needs no special handling — creating the watch emits the current state immediately.

### `wait_for_event` — the in-turn alternative

Blocks the current tool call until a matching event arrives, or times out:

```json
{ "eventType": "http.available", "source": "localhost:3000", "timeoutSeconds": 60 }
```

Returns `{"timedOut": false, "event": {...}}` or `{"timedOut": true}`.

**Live-only:** it sees events published *after* the call. For "might already be true" conditions, prefer `create_watch` (whose initial check emits the current state) plus either a short `wait_for_event` or a `create_rule`.

**When to use which:** `wait_for_event` keeps the turn open — fine for short waits (seconds). For anything longer, release-and-resume via `create_rule` is strictly better: no held turn, no timeout budget, no idle context.

### `get_events`

Query history: `{"type": "build.failed", "limit": 20}` → newest-first event list. Useful for "what happened while I was away".

### `cancel_rule`

`{"ruleId": "rule-..."}` — withdraw a pending wake-up.

## End-to-end example (what an agent actually does)

> User: "Deploy this once the stack is healthy."

1. Agent runs `runtimepulse exec --label stack -- docker compose up -d --wait` in the background, or calls `create_watch` for the key service.
2. Agent calls `create_rule`: on `exec.succeeded:stack` (or `docker.healthy:api`), resume me with *"Stack is healthy. Proceed with the deploy."*, oneShot.
3. Agent replies "Waiting for the stack — I'll continue automatically" and **ends its turn**.
4. The event fires; RuntimePulse resumes the session; the agent deploys; the completion itself becomes `continuation.completed:<label>` — available for further chaining.

## Operational notes

* The MCP process logs to stderr only; stdout is the protocol stream.
* Tool failures (daemon unreachable, invalid input) surface as tool errors the agent can read — they never kill the MCP connection.
* Headless resumes carry no permission flags: pre-approve the tools your chain needs in the agent's project settings, or steps will be denied mid-resume.
