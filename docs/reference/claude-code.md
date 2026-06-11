# Claude Code CLI — Session Resume & Headless Mode Reference

Verified on 2026-06-11 from official Claude Code documentation (code.claude.com/docs).

## 1. Resume command (headless)

```bash
claude --resume <session-id> -p "your prompt here" --output-format json
```

* `--resume <session-id|name>`: resume a specific session by UUID or user-assigned name.
* `-p` / `--print`: non-interactive mode; runs the prompt and exits.
* `--resume` + `-p` is fully supported and shown in official examples: `claude -p "Continue that review" --resume "$session_id"`.

Sources: [cli-reference](https://code.claude.com/docs/en/cli-reference.md), [headless](https://code.claude.com/docs/en/headless.md)

## 2. Session ids

* Format: standard UUID; named sessions also resolvable by name.
* Storage: `~/.claude/projects/<project>/<session-id>.jsonl` where `<project>` derives from the working directory the session was created in. One JSONL file per session (full transcript).
* Programmatic discovery: read `~/.claude/projects/` directly; `claude agents --json` lists active background sessions. Sessions created with `-p` don't appear in the interactive picker but can still be resumed by id.

Sources: [sessions](https://code.claude.com/docs/en/sessions.md), [cli-reference](https://code.claude.com/docs/en/cli-reference.md)

## 3. Output formats (`--output-format`)

| Format | Content |
|---|---|
| `text` (default) | Final response only |
| `json` | `result`, `session_id`, `usage`, `total_cost_usd`, model metadata |
| `stream-json` | NDJSON event stream (`system/init`, `stream_event`, …) with `session_id` per event |

`--json-schema '<schema>'` adds a `structured_output` field conforming to the schema.

Source: [headless](https://code.claude.com/docs/en/headless.md)

## 4. Exit codes

* `0` success; `1` on failure (tool denial, API error, validation error, max-turns exceeded).
* UNVERIFIED: exhaustive mapping of error types to codes is not documented. Treat any non-zero as failure.

Source: [cli-reference](https://code.claude.com/docs/en/cli-reference.md)

## 5. Working directory

Not strictly required to resume in the original directory, but the working directory determines which CLAUDE.md, settings, and MCP servers load. **Adapter rule: always resume in the session's recorded `repoPath`** so the original environment is restored.

Source: [sessions](https://code.claude.com/docs/en/sessions.md)

## 6. MCP registration

```bash
claude mcp add --transport stdio runtimepulse -- runtimepulse mcp   # local scope (default)
claude mcp add --transport stdio --scope user runtimepulse -- runtimepulse mcp
```

Project scope uses `.mcp.json` at repo root:

```json
{
  "mcpServers": {
    "runtimepulse": { "type": "stdio", "command": "runtimepulse", "args": ["mcp"] }
  }
}
```

Env expansion supported: `${VAR}`, `${VAR:-default}`. Manage with `claude mcp list|get|remove`.

Source: [mcp](https://code.claude.com/docs/en/mcp.md)

## 7. Channels (relevant to the planned optimization)

**Exists, research preview.** Channels are MCP servers that push events *into a running* Claude Code session (Telegram, Discord, iMessage plugins exist). Started via `claude --channels plugin:<name>`. Requires v2.1.80+, Anthropic auth; not available on Bedrock/Vertex/Foundry; teams must enable `channelsEnabled: true`.

This is the one agent platform with genuine inbound push — RuntimePulse could deliver events to a *running* Claude session through a channel instead of waiting for it to end. CLI resume remains the contractual fallback.

Source: [channels](https://code.claude.com/docs/en/channels.md)

## 8. Concurrency & permissions

* **Concurrent resumes of the same session interleave messages into one transcript** (documented). Validates RuntimePulse's per-session FIFO queue. `--fork-session` creates an isolated copy instead.
* Headless permission flags: `--dangerously-skip-permissions` (bypass all), `--allowedTools "A,B"` (whitelist), `--permission-mode dontAsk|acceptEdits`. The adapter should expose these as per-session/per-rule configuration, defaulting to *no* bypass.

Sources: [sessions](https://code.claude.com/docs/en/sessions.md), [permission-modes](https://code.claude.com/docs/en/permission-modes.md)
