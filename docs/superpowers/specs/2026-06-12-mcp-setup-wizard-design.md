# MCP Setup Wizard — Design

**Date:** 2026-06-12
**Status:** Approved

## Goal

`runtimepulse setup` detects the agent CLIs installed on the machine (Claude Code, Cursor, Codex, OpenCode) and registers RuntimePulse as their MCP server in one step — replacing the manual, per-agent `claude mcp add …` / config-file editing the README documents today. This makes the product's primary flow (agents self-registering wake-up rules via MCP) reachable without the user knowing each agent's registration mechanism.

## Decisions (owner-approved)

* **Command:** a single interactive wizard `runtimepulse setup`. Flags: `--all`/`--yes` (non-interactive), `--agent <name>` (target one), `--project` (cwd scope; default is user/global), `--dry-run`. Non-interactive automatically when stdin is not a TTY.
* **Default scope:** user/global (works in every project). `--project` writes to the current repo.
* **Mechanism:** native agent CLI where one exists, JSON-file merge otherwise. Verified split — only Claude and Codex expose an `mcp add` command; Cursor (`agent mcp` has only list/login/enable/disable) and OpenCode have none.
* **Binary reference:** the absolute path of the running executable (`os.Executable()`), not bare `runtimepulse` — robust regardless of PATH.

## Architecture

New package `internal/setup`, one `Agent` per supported CLI behind a small interface:

```go
type Scope int // ScopeUser, ScopeProject

type Status struct {
    Installed  bool   // binary found on PATH
    Registered bool   // RuntimePulse already registered for this agent
    Detail     string // e.g. resolved binary name, or why detection failed
}

type Agent interface {
    Name() string                                 // "claude" | "cursor" | "codex" | "opencode"
    Status(scope Scope) Status                    // detect + already-registered check
    Register(binPath string, scope Scope) error   // idempotent
    SupportsScope(scope Scope) bool               // codex: ScopeProject == false in v1
}
```

Two families:

* **`agents_native.go` — Claude, Codex.** `Register` runs the agent's own command as a subprocess through a `runCmd(name string, args ...string) error` seam (overridable in tests):
  * Claude user: `claude mcp add --transport stdio --scope user runtimepulse -- <binPath> mcp`; project: `--scope project`.
  * Codex: `codex mcp add runtimepulse -- <binPath> mcp` (writes `~/.codex/config.toml`; global only — see scope matrix). **We never hand-edit Codex's TOML.**
  * `Status` runs `<cli> mcp list` (or `mcp get runtimepulse`) and looks for `runtimepulse`; tolerant of an "already exists" error on `Register` (treated as success).
* **`agents_file.go` — Cursor, OpenCode.** `Register` calls the shared `jsonmerge.go` helper:
  * Cursor: user `~/.cursor/mcp.json`, project `<cwd>/.cursor/mcp.json`; set `mcpServers.runtimepulse = {"command": "<binPath>", "args": ["mcp"]}`.
  * OpenCode: user = the global config resolved via the OpenCode docs (verify exact path/name during implementation; candidate `~/.config/opencode/opencode.json` / `os.UserConfigDir()/opencode`), project `<cwd>/opencode.json`; set `mcp.runtimepulse = {"type": "local", "command": ["<binPath>", "mcp"], "enabled": true}`.
  * `Status.Registered` = the `runtimepulse` key already present in the parsed config.

`jsonmerge.go` — the riskiest, most-tested unit:
1. Read the file; absent → start from `{}`.
2. Parse into `map[string]any`; a parse error aborts with a clear message (never clobber a file we can't understand).
3. Ensure the nested object (`mcpServers` / `mcp`) exists, set the `runtimepulse` key, preserve every other key (incl. `$schema`).
4. Marshal indented; write atomically (temp in the same dir + `os.Rename`), creating parent dirs (0700).

`setup.go` — the orchestration: `DetectAll(scope) []AgentStatus`, plus a `Run` that registers a selected set and returns per-agent results.

`cmd/runtimepulse/cmd_setup.go` — the CLI: resolve `os.Executable()`, detect, render the status table, prompt (TTY) or honor `--all`/`--yes`, register each selected agent, print a result table. Logic stays thin; the package holds the testable behavior.

## Scope matrix

| Agent | User/global | Project (`--project`) |
|---|---|---|
| Claude | `claude mcp add --scope user` | `claude mcp add --scope project` (`.mcp.json`) |
| Codex | `codex mcp add` (`~/.codex/config.toml`) | **not supported in v1** — TOML-only project config; wizard registers globally and says so |
| Cursor | `~/.cursor/mcp.json` merge | `<cwd>/.cursor/mcp.json` merge |
| OpenCode | global config merge | `<cwd>/opencode.json` merge |

`SupportsScope(ScopeProject)` returns false for Codex; the wizard surfaces "codex: project scope unsupported, registered globally" rather than failing.

## UX

```
$ runtimepulse setup
Scanning for installed agent CLIs…
  ✓ claude     found            not registered
  ✓ codex      found            already registered
  ✗ cursor     not found
  ✓ opencode   found            not registered

Register RuntimePulse (MCP) for claude, opencode? [Y/n]

  claude    ✓ registered (user scope)
  opencode  ✓ registered (~/.config/opencode/opencode.json)

Done. Restart any running agent sessions to pick up the new MCP server.
```

`--dry-run` prints the same plan and the exact command / file path it *would* use, writing nothing.

## Error handling

* Each agent is independent: one failure (CLI errored, config unparseable, permission denied) is reported in its result row and does not abort the others. The command's exit code is nonzero if any selected agent failed.
* JSON merge is atomic and refuses to touch a file it can't parse.
* Detection uses `exec.LookPath`; Cursor checks `agent` then legacy `cursor-agent`.
* Native-CLI "already registered" is not an error — reported as a skip.

## Testing

* **`jsonmerge` (unit):** absent file → created with parents; existing file with an unrelated server → runtimepulse added, the other preserved; `$schema` preserved; re-run is idempotent (no duplicate, byte-stable); unparseable file → error, original untouched. Temp dirs (platform-aware, per the Windows test conventions).
* **File agents (unit):** Cursor/OpenCode `Register` against a temp config path produce the exact expected JSON shape; `Status.Registered` flips after registration.
* **Native agents (unit):** the `runCmd` seam is injected with a mock (mockexe pattern) that records argv to a file; assert Claude/Codex are invoked with the exact `mcp add … -- <binPath> mcp` arguments for each scope. Codex `ScopeProject` returns `SupportsScope=false`.
* **Smoke:** with mock `claude`/`codex` on PATH (mockexe) and temp HOME/config, `runtimepulse setup --all --dry-run` lists the plan; `runtimepulse setup --all` registers and a re-run reports all "already registered". `has`/`count` helpers only — never `| grep -q` under pipefail.
* Windows gate: `GOOS=windows go build/vet`; config paths via `os.UserHomeDir`/`os.UserConfigDir` are cross-platform.

## Docs

README "Install" gains a one-liner: after install, run `runtimepulse setup` to register with your agents. `docs/usage/mcp.md` leads with `runtimepulse setup` and keeps the manual per-agent commands as the fallback/reference. `docs/PROJECT.md` §7.1 lists the `setup` command.

## Out of scope (deliberate)

* An `unregister`/`setup --remove` command (symmetric but YAGNI for v1).
* Editing Codex's TOML for project scope (delegated entirely to `codex mcp add`).
* Detecting agent *versions* or validating the registration actually works end-to-end (the agent's own `mcp list` is the user's check).
* Non-stdio MCP transports, remote MCP, or auth headers — RuntimePulse's MCP server is local stdio only.
