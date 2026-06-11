# Codex CLI — Session Resume Reference

Verified on 2026-06-11 against official docs (developers.openai.com/codex) and the openai/codex GitHub repo.

## 1. Binary, install, version

* Binary: `codex`. Install: `npm install -g @openai/codex` or `brew install --cask codex`.
* Version: `codex --version` (prints e.g. `codex-cli 0.121.0`).

Source: [github.com/openai/codex](https://github.com/openai/codex)

## 2. Resume command (non-interactive)

```bash
codex exec resume <SESSION_ID> --json -C <repo> "<prompt>"
```

* `codex exec resume <id> "prompt"` — resume a specific exec session by id (official). `--last` resumes the most recent session *from the current working directory*; `--all` widens to sessions outside cwd.
* `codex resume [...]` is the *interactive* (TUI) variant — not for adapters.
* Resumed sessions preserve "the original transcript, plan history, and approvals".
* `codex exec` expects a git repo by default; `--skip-git-repo-check` bypasses.

Sources: [cli/reference](https://developers.openai.com/codex/cli/reference), [noninteractive](https://developers.openai.com/codex/noninteractive), [cli/features](https://developers.openai.com/codex/cli/features)

## 3. Session ids

* **No `codex sessions list` subcommand exists.** Discovery: interactive picker, `/status` in-session, or files on disk.
* On disk: `~/.codex/sessions/YYYY/MM/DD/rollout-<timestamp>-<id>.jsonl` (path confirmed officially; filename layout community-verified, UNVERIFIED officially).
* Id format: not documented; auto-generated, cannot be set. **Treat as opaque string** parsed from rollout filename or `--json` output (`thread.started` event).

Sources: [cli/features](https://developers.openai.com/codex/cli/features), [discussion #3827](https://github.com/openai/codex/discussions/3827)

## 4. Output formats (exec mode)

* Default: progress → stderr, final message → stdout.
* `--json`: NDJSON events (`thread.started`, `turn.completed`, `item.completed`).
* `-o/--output-last-message <file>`: final message to file (docs recommend pairing with `--json` in CI).
* `--output-schema <schema.json>`: schema-conforming final response.

Source: [noninteractive](https://developers.openai.com/codex/noninteractive)

## 5. Exit codes

* Documented only as "exits non-zero if the task submission fails".
* Known bug history: SIGINT in exec mode returned 0 ([issue #4721](https://github.com/openai/codex/issues/4721)). **Adapter rule: treat any non-zero as failure, but also parse `--json` events; don't rely on specific codes.**

## 6. Working directory, sandbox, approvals

* `-C/--cd <path>`: set working directory. `--last` resolution is cwd-scoped — always pass `-C <repoPath>`.
* `--sandbox read-only|workspace-write|danger-full-access` (exec default: read-only — adapters likely need `workspace-write`).
* `--ask-for-approval untrusted|on-request|never`. `--full-auto` is deprecated (prefer `--sandbox workspace-write`).
* `--ephemeral` disables persistence — **never use in adapters** (kills resumability).

Source: [cli/reference](https://developers.openai.com/codex/cli/reference)

## 7. MCP registration

```bash
codex mcp add runtimepulse -- runtimepulse mcp
```

Config `~/.codex/config.toml` (or project `.codex/config.toml`):

```toml
[mcp_servers.runtimepulse]
command = "runtimepulse"
args = ["mcp"]
```

Optional: `env`, `startup_timeout_sec`, `tool_timeout_sec`, `enabled`.

Source: [codex/mcp](https://developers.openai.com/codex/mcp)

## 8. Headless auth

* Default: `codex login` (ChatGPT OAuth). API key: `codex login --with-api-key`. No-browser: `codex login --device-auth` (beta) or `--with-access-token` (Enterprise).
* Credentials at `~/.codex/auth.json`. Plain `OPENAI_API_KEY` env var sufficing without prior login: UNVERIFIED.

Source: [codex/auth](https://developers.openai.com/codex/auth)
