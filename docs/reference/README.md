# Agent CLI Reference

Distilled references for the four agent CLIs RuntimePulse adapters target. These are **not** copies of vendor docs — each file answers exactly the questions an adapter implementation needs:

1. Binary name, install, version check
2. Non-interactive resume command (the adapter's core call)
3. Session id discovery (format + on-disk location)
4. Machine-readable output formats
5. Exit code contract
6. Working directory requirements
7. MCP server registration (how agents will reach RuntimePulse's MCP tools)
8. Headless auth

| Agent | File | Resume command shape |
|---|---|---|
| Claude Code | [claude-code.md](claude-code.md) | `claude --resume <id> -p "<prompt>" --output-format json` |
| Cursor CLI | [cursor-cli.md](cursor-cli.md) | `agent -p --resume <id> --output-format json --workspace <repo> "<prompt>"` |
| Codex CLI | [codex-cli.md](codex-cli.md) | `codex exec resume <id> --json -C <repo> "<prompt>"` |
| OpenCode | [opencode.md](opencode.md) | `opencode run --session <id> --dir <repo> --format json "<prompt>"` |

**Staleness warning:** all content was verified against official docs on **2026-06-11**. These CLI surfaces drift fast — claims marked UNVERIFIED were not confirmed by official sources, and even verified flags must be re-checked by each adapter's `Validate()` smoke test at runtime. When a discrepancy is found, fix the reference file in the same commit as the adapter fix.

**Findings that changed the spec** (already applied to `docs/PROJECT.md`):

* Cursor's binary is now `agent` (installed to `~/.local/bin`), not `cursor-agent`; older installs may still have the old name — adapters should detect both.
* Concurrent resumes of one Claude Code session interleave into one transcript — independently validates the per-session FIFO queue decision.
* Claude "Channels" exists (research preview) but is an *inbound event push* mechanism for running sessions, not session sharing — relevant to the planned Channels optimization.
* OpenCode has a first-class HTTP server mode (`opencode serve`, `POST /session/:id/message`) that is likely a more robust adapter path than CLI spawning; its CLI exit codes are historically unreliable (exit 0 on error), so the adapter must parse JSON events instead.
