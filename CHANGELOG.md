# Changelog

All notable changes to RuntimePulse are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.3.0] - 2026-06-12
### Added
- `runtimepulse setup` can optionally install the "release-and-resume" guidance into
  each agent's global instruction file (`--rules` / `--no-rules`, opt-in prompt
  defaulting to no). Claude → `~/.claude/CLAUDE.md`, Codex → `~/.codex/AGENTS.md`,
  opencode → `~/.config/opencode/AGENTS.md`; Cursor's text is printed to paste into
  Settings → Rules. The write is an idempotent, marker-delimited block; `--dry-run` previews.

## [1.2.0] - 2026-06-12
### Added
- Automatic retention sweeper: idle sessions (7 days, with no active rule and not running),
  terminal continuations and events (30 days) are garbage-collected hourly and on boot.
- MCP server instructions and tool descriptions now teach the release-and-resume pattern
  (arm a rule, end the turn, get resumed — never poll).

## [1.1.1] - 2026-06-12
### Fixed
- Resume failure summaries now surface the agent's stderr error instead of being masked
  by unrelated stdout noise.

## [1.1.0] - 2026-06-12
### Added
- `runtimepulse setup` wizard: detects installed agent CLIs and registers RuntimePulse as
  their MCP server — native `mcp add` for Claude/Codex, atomic JSON merge for Cursor/opencode.
- Interactive per-agent picker, comma-separated `--agent` selection, and scope reporting.

## [1.0.0] - 2026-06-12
### Added
- First public release. Full spec roadmap: core engine (store/bus/engine/daemon/CLI),
  watchers (http, tcp, file, process, docker, git + the `exec` wrapper), supervised
  continuation dispatcher with multi-step chaining, MCP server (create_watch/create_rule/
  wait_for_event/get_events/cancel_rule), all four agent adapters, workflow YAML compiler
  (`runtimepulse apply`), and the opt-in loopback WebSocket event stream.
- Windows support (Win10 1803+) with a 3-OS CI matrix.
- Distribution: tag-driven GoReleaser (6 archives + SHA256SUMS), checksum-verifying
  install script, and a Homebrew cask.

[1.3.0]: https://github.com/tufantunc/RuntimePulse/releases/tag/v1.3.0
[1.2.0]: https://github.com/tufantunc/RuntimePulse/releases/tag/v1.2.0
[1.1.1]: https://github.com/tufantunc/RuntimePulse/releases/tag/v1.1.1
[1.1.0]: https://github.com/tufantunc/RuntimePulse/releases/tag/v1.1.0
[1.0.0]: https://github.com/tufantunc/RuntimePulse/releases/tag/v1.0.0
