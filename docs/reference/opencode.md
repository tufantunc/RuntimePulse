# OpenCode CLI — Session Resume Reference

Verified on 2026-06-11 against official docs (opencode.ai/docs) and the anomalyco/opencode GitHub repo.

## 1. Binary, install, version

* Binary: `opencode`. Install: `curl -fsSL https://opencode.ai/install | bash`, `npm install -g opencode-ai`, or `brew install anomalyco/tap/opencode` (tap has newest releases).
* Version: `opencode --version`.

Source: [docs](https://opencode.ai/docs/), [cli](https://opencode.ai/docs/cli/)

## 2. Resume command (non-interactive)

```bash
opencode run --session <session-id> --dir <repo> --format json "<prompt>"
```

* `--session/-s <id>`: "Session ID to continue" (id only, not title). `--continue/-c`: latest session. `--fork`: fork when continuing.
* `--dir`: directory to run in. `--dangerously-skip-permissions` exists for unattended runs.
* `--attach http://localhost:4096`: run against an already-running server instead of spawning a fresh engine.

Source: [cli](https://opencode.ai/docs/cli/), [issue #12404](https://github.com/anomalyco/opencode/issues/12404)

## 3. Session ids

* Format: `ses_` prefix + alphanumeric (e.g. `ses_3cf7dd8d4ffeUPfENpVxfFojZ2`); messages use `msg_`. Exact generation scheme UNVERIFIED — treat as opaque.
* List: `opencode session list --format json` (`-n <N>` limits count).
* On disk: `~/.local/share/opencode/project/<project-slug>/storage/session/{projectHash}/{sessionID}.json` (Git repos) or `project/global/storage/` (non-Git). Override root with `OPENCODE_DATA_DIR`.

Sources: [cli](https://opencode.ai/docs/cli/), [troubleshooting](https://opencode.ai/docs/troubleshooting/)

## 4. Output formats

* `--format default|json` — json emits raw JSON events. Event schema not officially enumerated; observed: `step_start`, `text`, `step_finish` (carries `tokens`, `cost`, `reason`). Treat schema as UNVERIFIED beyond these names; known bug: json mode can exit before final `step_finish` ([issue #26855](https://github.com/anomalyco/opencode/issues/26855)).
* `--print-logs` / `--log-level` are diagnostics (stderr), not response format.

Source: [cli](https://opencode.ai/docs/cli/)

## 5. Exit codes — UNRELIABLE

No documented contract, and a documented history of `opencode run` exiting `0` on session/API/model errors ([#2489](https://github.com/anomalyco/opencode/issues/2489), [#14551](https://github.com/anomalyco/opencode/issues/14551), [#15558](https://github.com/anomalyco/opencode/issues/15558)). **Adapter rule: never trust exit codes alone — parse the JSON event stream for success/failure.**

## 6. Working directory

Sessions are stored per-project (keyed by repo). Resuming from a different directory can yield "Session not found" ([#12002](https://github.com/anomalyco/opencode/issues/12002)). **Adapter rule: always pass `--dir <repoPath>`.**

## 7. Server / HTTP API mode — preferred adapter path

`opencode serve` starts a headless server (default `127.0.0.1:4096`; `--port`, `--hostname`; basic auth via `OPENCODE_SERVER_PASSWORD`; OpenAPI 3.1 spec at `/doc`).

| Action | Endpoint |
|---|---|
| List sessions | `GET /session` |
| Create session | `POST /session` |
| **Resume: send prompt** | `POST /session/:id/message` |
| Fire-and-forget prompt | `POST /session/:id/prompt_async` |
| Messages / fork / revert / diff | `GET /session/:id/message`, `POST /session/:id/fork`, … |

SDK detail: `parts: [{type:"text", text:"…"}]`; `noReply: true` injects context **without** triggering a response. For a long-running daemon this path avoids per-event process spawn and the exit-code ambiguity above — **the OpenCode adapter should prefer serve+HTTP when a server is running, with CLI `run --session` as fallback.**

Sources: [server](https://opencode.ai/docs/server/), [sdk](https://opencode.ai/docs/sdk/)

## 8. MCP registration (`opencode.json`)

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "runtimepulse": {
      "type": "local",
      "command": ["runtimepulse", "mcp"],
      "enabled": true
    }
  }
}
```

Remote variant: `{"type":"remote","url":"…","headers":{…}}`. Fields: `environment`, `timeout` (ms), `oauth`.

Source: [mcp-servers](https://opencode.ai/docs/mcp-servers/)

## 9. Headless auth

* `opencode auth login|list|logout`; credentials in `~/.local/share/opencode/auth.json`.
* Provider env vars work headlessly (`ANTHROPIC_API_KEY`, etc.); `opencode.json` supports `{env:VAR}` interpolation.

Sources: [cli](https://opencode.ai/docs/cli/), [providers](https://opencode.ai/docs/providers/)
