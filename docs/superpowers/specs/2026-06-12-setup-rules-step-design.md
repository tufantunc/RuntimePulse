# Setup Rules Step — Design

**Date:** 2026-06-12
**Status:** Approved
**Topic:** An optional `runtimepulse setup` step that installs the
"release-and-resume" guidance into each agent's global instruction layer.

## 1. Problem

The MCP tools are *available* to an agent, but nothing makes the agent reach
for them. The release-and-resume pattern (arm a rule, end the turn, get
resumed — never poll) is counter to an agent's default reflex (block, sleep,
or ask). v1.2.0 added server-level MCP `Instructions`, but those are only a
nudge and are not surfaced to the model by every host.

The reliable lever is the agent's **global instruction layer**, which the
agent reads on every session, host-independent. Editing a per-project file
in every repo is impractical; a one-time global install per agent solves it.

This feature lets `runtimepulse setup` optionally write that guidance to each
agent's global instruction file.

## 2. Research findings (load-bearing)

Global instruction layer per agent:

| Agent    | Global location                          | Writable from CLI? |
|----------|------------------------------------------|--------------------|
| Claude   | `~/.claude/CLAUDE.md`                     | Yes (plain file)   |
| Codex    | `~/.codex/AGENTS.md`                      | Yes (plain file)   |
| opencode | `~/.config/opencode/AGENTS.md`           | Yes (plain file)   |
| Cursor   | Settings → Rules → User Rules            | **No** — GUI only  |

**Cursor cannot be automated.** Its global User Rules live in a hot SQLite
app store (`~/Library/Application Support/Cursor/User/globalStorage/state.vscdb`),
not a plain file; writing it directly risks corruption and the IDE owns/
overwrites the value. Furthermore, the headless `agent -p --resume` mode
RuntimePulse uses to resume sessions reads **only** project-root
`.cursor/rules/*.mdc`, `AGENTS.md`, and `CLAUDE.md` — global User Rules are
not consulted in that mode at all. (Sources: cursor.com/docs/rules,
cursor.com/docs/cli/using; local verification of `cursor-agent` v2026.06.12,
which exposes no `rules set` command.)

**Decision (Q1):** for Cursor, print the rule text with instructions to paste
into Settings → Rules. Do not write any file. Project-scoped `.cursor/rules`
writing is explicitly out of scope (it reintroduces the per-project burden
the user wants to avoid).

## 3. Behavior

The rules step runs **after** MCP registration, over the **same selected
agents and scope** as the registration it follows.

- **Interactive:** after MCP registration completes, ask once:
  *"Add the release-and-resume guidance to your agents' global instructions? [y/N]"*
  Default is **N** — writing to a user's instruction file is opt-in.
- **Non-interactive:** `--rules` writes; `--no-rules` skips. With `--yes`/`--all`
  and neither flag given, the safe default is **skip** (do not write).
- **`--dry-run`:** writes nothing. File agents report "would write …"; Cursor
  still prints the paste instructions.

## 4. The rule content

A single source of truth: `const releaseResumeRule` in the setup package,
mirroring the server-level MCP `Instructions` (internal/mcpserver/server.go).

File agents receive it wrapped in a managed marker block:

```markdown
<!-- runtimepulse:begin (managed — do not edit inside) -->
## RuntimePulse: release-and-resume

When you need to wait for a runtime condition (a service becoming healthy, a
build finishing, a file appearing), do NOT poll, sleep, or block. Instead:

1. Call the `create_rule` MCP tool with your own session id (pass agent +
   repoPath to self-register; use oneShot:true for wake-me-once).
2. If a watcher is needed, call `create_watch` AFTER the rule (a new watch
   emits current state immediately, so the rule must already exist).
3. End your turn. RuntimePulse resumes your session with the event as a new
   prompt when the condition is met.

Use `wait_for_event` only for conditions expected within seconds.
<!-- runtimepulse:end -->
```

Cursor receives the same body **without** the marker comments (plain text for
the GUI).

## 5. Idempotent write — `mergeMarkerBlock`

A new helper alongside `mergeJSONServer`, using the same atomic temp+rename
pattern (CreateTemp in the target dir → write → Rename):

- **File missing:** create it (and parent dirs, 0o700), write the block.
- **Block present** (both markers found): replace the text between the
  markers — version bumps refresh content in place, no duplication.
- **Block absent but file non-empty:** append the block at the end, separated
  by a blank line, leaving existing content untouched.
- **Atomic:** temp file in the same directory + `os.Rename`; deferred cleanup
  removes the temp only on failure.

Action reported: `written` (new file), `updated` (block replaced),
`unchanged` (block already identical), or `appended` (added to existing file).

## 6. Interface & components

A small, separate capability so `Register` stays focused:

```go
// RuleWriter knows how an agent applies the global release-and-resume rule.
type RuleWriter interface {
    WriteRule(scope Scope, dryRun bool) RuleResult
}

type RuleResult struct {
    Action string // written | updated | appended | unchanged | manual | would-write
    Path   string // file written, or "" for manual
    Manual bool   // true for Cursor — caller prints PasteText
    Err    error
}
```

- **claude / codex / opencode** → `fileRuleWriter{home}`: knows its global path,
  calls `mergeMarkerBlock`. Paths are `home`-based for testability (mirrors the
  existing `cursorAgent{home,cwd}` pattern); tests pass a `t.TempDir()` home.
- **cursor** → `manualRuleWriter`: writes nothing, returns `Manual: true` and
  the marker-less paste text.

Each concrete agent type gains a `WriteRule` method; the four are assembled in
`Registry` exactly as today.

## 7. CLI wiring

`cmd_setup.go` gains `--rules` / `--no-rules` bool flags and the post-
registration prompt. Output follows the existing ✓/✗/• convention:

```
claude   ✓ rules written (~/.claude/CLAUDE.md)
codex    ✓ rules updated (~/.codex/AGENTS.md)
opencode • rules unchanged (~/.config/opencode/AGENTS.md)
cursor   • rules: paste manually (see below)
```

The Cursor paste block is printed once after the per-agent lines.

## 8. Testing

- `mergeMarkerBlock`: new-file / append-to-existing / replace-block /
  idempotent-rerun / preserves-surrounding-content (matrix style, mirroring
  jsonmerge_test.go).
- `fileRuleWriter`: `t.TempDir()` home → asserts file content + action.
- `manualRuleWriter`: returns Manual + text, writes no file.
- Command level: `--rules` / `--no-rules` / interactive default-N flow;
  `--dry-run` writes nothing.
- Gates: `gofmt -l .` empty, `go vet`, `GOOS=windows go build/vet`, and a
  light `setup --rules --dry-run` assertion in smoke.sh.

## 9. Out of scope (YAGNI)

- No `--remove-rules` / uninstall.
- No per-rule customization or templating.
- No project-scoped `.cursor/rules` writing (Q1 decision).
- No change to MCP registration behavior; the rules step is purely additive
  and runs after it.
