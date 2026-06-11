# Cursor CLI (`agent`) — Session Resume Reference

Verified on 2026-06-11 against official Cursor documentation (cursor.com/docs/cli).

> **Naming:** current docs name the binary `agent` (installed to `~/.local/bin`). The launch blog and older material use `cursor-agent`. Whether the old name remains an alias is UNVERIFIED — **the adapter should detect `agent` first, fall back to `cursor-agent`.**

## 1. Binary, install, version

* Install: `curl https://cursor.com/install -fsS | bash` (no npm distribution).
* Version: `agent --version`; `agent about` for version + account. Auto-updates by default (`agent update` to force).

Source: [installation](https://cursor.com/docs/cli/installation)

## 2. Resume command (non-interactive)

```bash
agent -p --resume <chat-id> --output-format json --workspace <repo> "<prompt>"
```

* `--resume [chatId]` resumes a chat session; `--continue` is an alias for `--resume=-1` (latest).
* `-p` / `--print`: non-interactive mode. Prompt is a positional argument.
* **UNVERIFIED:** no official example combines `-p` with `--resume`; both are documented global options. The adapter's `Validate()` must smoke-test the combination (e.g. `agent create-chat` → resume it).
* Unattended file edits require `-f` / `--force` (alias `--yolo`); `--trust` trusts the workspace without prompting (headless only).

Sources: [parameters](https://cursor.com/docs/cli/reference/parameters), [headless](https://cursor.com/docs/cli/headless)

## 3. Session ids

* Format: UUID (per the documented `session_id` field in JSON output).
* `agent ls` lists previous chat sessions (machine-readable output for `ls`: UNVERIFIED).
* `agent create-chat` creates an empty chat and returns its id — useful for pre-registering sessions.
* On-disk storage `~/.cursor/chats/`: forum-sourced, UNVERIFIED implementation detail. **Prefer capturing `session_id` from JSON output over disk scraping.**

Sources: [parameters](https://cursor.com/docs/cli/reference/parameters), [output-format](https://cursor.com/docs/cli/reference/output-format)

## 4. Output formats (`--output-format`, requires `--print`)

* `text` (default): final message only.
* `json`: single object — `{"type":"result","subtype":"success","is_error":false,"duration_ms":…,"result":"…","session_id":"<uuid>"}`.
* `stream-json`: NDJSON events (init, user/assistant messages, tool calls) ending with a terminal `result` event; `--stream-partial-output` adds deltas.

Source: [output-format](https://cursor.com/docs/cli/reference/output-format)

## 5. Exit codes

* `0` on success; non-zero + stderr message on failure. In `stream-json`, a failed run may end without a terminal event.
* UNVERIFIED: no documented table of distinct non-zero codes.

Source: [output-format](https://cursor.com/docs/cli/reference/output-format)

## 6. Working directory

* Defaults to cwd as repository root; `--workspace <path>` targets a repo from anywhere.
* UNVERIFIED: whether resume requires the original workspace. **Adapter rule: always pass `--workspace <repoPath>` from the session record.**

Source: [using](https://cursor.com/docs/cli/using)

## 7. MCP registration

* Same config as the editor: project `.cursor/mcp.json`, global `~/.cursor/mcp.json`.

```json
{ "mcpServers": { "runtimepulse": { "command": "runtimepulse", "args": ["mcp"] } } }
```

* Supports `${env:NAME}`, `${workspaceFolder}`, `envFile`. CLI: `agent mcp list|list-tools|login|enable|disable`. Headless approval: `--approve-mcps`.

Sources: [cli/mcp](https://cursor.com/docs/cli/mcp), [context/mcp](https://cursor.com/docs/context/mcp)

## 8. Headless auth

* Interactive: `agent login` / `agent status`.
* Headless/CI: `CURSOR_API_KEY` env var (key from Cursor Dashboard) or `--api-key <key>` per invocation.

Source: [authentication](https://cursor.com/docs/cli/reference/authentication)
