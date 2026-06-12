# Setup Rules Step Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional `runtimepulse setup` step that installs the "release-and-resume" guidance into each agent's global instruction file (Cursor gets a paste-into-Settings printout, since its global rules are not file-writable).

**Architecture:** A new `internal/setup/rules.go` holds the rule text, an idempotent marker-block file writer (`mergeMarkerBlock`, sharing a new `atomicWrite` helper extracted from `jsonmerge.go`), a small `RuleWriter` interface with `fileRuleWriter` (claude/codex/opencode) and `manualRuleWriter` (cursor) implementations behind a `ruleWriterFor` factory, and a `WriteRules` orchestrator mirroring the existing `Run`. The `setup` cobra command gains `--rules` / `--no-rules` flags and an opt-in (default-N) prompt that runs after MCP registration.

**Tech Stack:** Go, cobra, the existing `internal/setup` package patterns (home-injected agents, atomic temp+rename writes, runFunc/queryFunc seams).

**Spec:** `docs/superpowers/specs/2026-06-12-setup-rules-step-design.md`

**Design note (refinement of spec §6):** rule writers are a *standalone* `RuleWriter` set built by a `ruleWriterFor(name, home)` factory, decoupled from the `Agent` interface — `claudeAgent`/`codexAgent` are zero-value structs with no `home` field, so adding `WriteRule` methods to them would force a struct change and touch the MCP-registration path. The factory keeps the existing `Agent` interface untouched (smaller blast radius) while producing the same four behaviors. Rule install is always **global/user** scope (the feature's whole point); `WriteRule` takes no `Scope`.

---

### Task 1: Verify opencode's global instruction path

No code in this task — a verification gate. The plan relies on opencode reading `~/.config/opencode/AGENTS.md` globally. Confirm before Task 3 hard-codes it.

- [ ] **Step 1: Check opencode docs and local install**

Run:
```bash
which opencode && opencode --version
ls -la ~/.config/opencode/ 2>/dev/null
```
Then fetch the opencode rules/config docs (https://opencode.ai/docs/rules and https://opencode.ai/docs/config) and confirm: does opencode read a **global** `AGENTS.md` at `~/.config/opencode/AGENTS.md` (or via the config `instructions` array)?

- [ ] **Step 2: Record the verified path**

Expected outcome: confirm `~/.config/opencode/AGENTS.md` is the global rules file opencode reads. If the docs instead require registering the file via the `instructions` array in `~/.config/opencode/opencode.json`, OR a different path, write the corrected path/mechanism here:

> opencode global rules mechanism (verified 2026-06-12, opencode 1.17.4 + opencode.ai/docs/rules):
> **(A)** opencode auto-reads a bare global `~/.config/opencode/AGENTS.md` on startup (precedence: local AGENTS.md/CLAUDE.md up from cwd → global `~/.config/opencode/AGENTS.md` → `~/.claude/CLAUDE.md` fallback). The `instructions` array in opencode.json is a separate optional mechanism and is NOT required. The opencode rules writer is a **plain file append to `~/.config/opencode/AGENTS.md`** — no opencode.json change. Task 3's `ruleWriterFor` opencode case stands as written.

Task 3's `ruleWriterFor` opencode case MUST use whatever this step confirms. If opencode needs an `instructions`-array registration rather than a bare global `AGENTS.md`, note it — that changes the opencode writer from a plain file append to a file-write **plus** a `mergeJSONServer`-style config entry. Default assumption if docs confirm it: plain `~/.config/opencode/AGENTS.md`.

- [ ] **Step 3: Commit the finding into the plan**

```bash
git add docs/superpowers/plans/2026-06-12-setup-rules-step.md
git commit -m "plan: verify opencode global AGENTS.md path for rules step"
```

---

### Task 2: Rule content + atomicWrite refactor + mergeMarkerBlock

**Files:**
- Create: `internal/setup/rules.go`
- Create: `internal/setup/rules_test.go`
- Modify: `internal/setup/jsonmerge.go` (extract `atomicWrite`, reuse it)

- [ ] **Step 1: Write the failing tests for mergeMarkerBlock**

Create `internal/setup/rules_test.go`:

```go
package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeMarkerBlockNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	action, err := mergeMarkerBlock(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != "written" {
		t.Fatalf("action = %q, want written", action)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if !strings.Contains(got, ruleBeginMarker) || !strings.Contains(got, ruleEndMarker) {
		t.Fatalf("markers missing:\n%s", got)
	}
	if !strings.Contains(got, "release-and-resume") {
		t.Fatalf("rule body missing:\n%s", got)
	}
}

func TestMergeMarkerBlockAppendsPreservingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	original := "# My rules\n\nUse tabs, not spaces.\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	action, err := mergeMarkerBlock(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != "appended" {
		t.Fatalf("action = %q, want appended", action)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if !strings.HasPrefix(got, original) {
		t.Fatalf("existing content not preserved verbatim at start:\n%s", got)
	}
	if !strings.Contains(got, ruleBeginMarker) {
		t.Fatalf("block not appended:\n%s", got)
	}
}

func TestMergeMarkerBlockReplacesInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	stale := "# top\n\n" + ruleBeginMarker + "\nOLD STALE TEXT\n" + ruleEndMarker + "\n\n# bottom\n"
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	action, err := mergeMarkerBlock(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != "updated" {
		t.Fatalf("action = %q, want updated", action)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if strings.Contains(got, "OLD STALE TEXT") {
		t.Fatalf("stale block not replaced:\n%s", got)
	}
	if !strings.Contains(got, "# top") || !strings.Contains(got, "# bottom") {
		t.Fatalf("surrounding content lost:\n%s", got)
	}
	if strings.Count(got, ruleBeginMarker) != 1 {
		t.Fatalf("expected exactly one block, got %d:\n%s", strings.Count(got, ruleBeginMarker), got)
	}
}

func TestMergeMarkerBlockIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	if _, err := mergeMarkerBlock(path, false); err != nil {
		t.Fatal(err)
	}
	action, err := mergeMarkerBlock(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != "unchanged" {
		t.Fatalf("second run action = %q, want unchanged", action)
	}
}

func TestMergeMarkerBlockDryRunWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	action, err := mergeMarkerBlock(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if action != "would-write" {
		t.Fatalf("action = %q, want would-write", action)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry-run created the file")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/setup/ -run TestMergeMarkerBlock -v`
Expected: FAIL — `undefined: mergeMarkerBlock`, `ruleBeginMarker`, `ruleEndMarker`.

- [ ] **Step 3: Create rules.go with content + mergeMarkerBlock**

Create `internal/setup/rules.go`:

```go
package setup

import (
	"os"
	"strings"
)

// releaseResumeRuleBody is the single source of truth for the guidance,
// mirroring the server-level MCP Instructions in internal/mcpserver/server.go.
const releaseResumeRuleBody = `## RuntimePulse: release-and-resume

When you need to wait for a runtime condition (a service becoming healthy, a
build finishing, a file appearing), do NOT poll, sleep, or block. Instead:

1. Call the ` + "`create_rule`" + ` MCP tool with your own session id (pass agent +
   repoPath to self-register; use oneShot:true for wake-me-once).
2. If a watcher is needed, call ` + "`create_watch`" + ` AFTER the rule (a new watch
   emits current state immediately, so the rule must already exist).
3. End your turn. RuntimePulse resumes your session with the event as a new
   prompt when the condition is met.

Use ` + "`wait_for_event`" + ` only for conditions expected within seconds.`

const (
	ruleBeginMarker = "<!-- runtimepulse:begin (managed — do not edit inside) -->"
	ruleEndMarker   = "<!-- runtimepulse:end -->"
)

// managedBlock is the canonical marker-delimited block (no trailing newline).
func managedBlock() string {
	return ruleBeginMarker + "\n" + strings.TrimSpace(releaseResumeRuleBody) + "\n" + ruleEndMarker
}

// mergeMarkerBlock ensures the managed rule block is present in the file at
// path, writing atomically. Returns the action: "written" (new file),
// "updated" (stale block replaced), "appended" (added to existing content),
// or "unchanged". With dryRun it writes nothing and returns "would-write"
// when a write would occur, "unchanged" otherwise.
func mergeMarkerBlock(path string, dryRun bool) (string, error) {
	block := managedBlock()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	var out, action string
	switch {
	case os.IsNotExist(err):
		out, action = block+"\n", "written"
	default:
		content := string(raw)
		b := strings.Index(content, ruleBeginMarker)
		e := strings.Index(content, ruleEndMarker)
		if b >= 0 && e >= b {
			rebuilt := content[:b] + block + content[e+len(ruleEndMarker):]
			if rebuilt == content {
				return "unchanged", nil
			}
			out, action = rebuilt, "updated"
		} else {
			out = strings.TrimRight(content, "\n") + "\n\n" + block + "\n"
			action = "appended"
		}
	}

	if dryRun {
		return "would-write", nil
	}
	if err := atomicWrite(path, []byte(out)); err != nil {
		return "", err
	}
	return action, nil
}
```

- [ ] **Step 4: Extract atomicWrite in jsonmerge.go and reuse it**

In `internal/setup/jsonmerge.go`, replace the temp+rename block at the end of `mergeJSONServer` (lines 42–58) so it calls a shared helper, and add the helper. The new tail of `mergeJSONServer` becomes:

```go
	return atomicWrite(path, out)
}

// atomicWrite writes data to path via a temp file in the same directory
// plus os.Rename, creating parent dirs (0o700). The rename is atomic on
// the same filesystem; the temp is cleaned up if the rename never happens.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".rp-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
```

Delete the now-duplicated `os.MkdirAll`/`CreateTemp`/`Write`/`Close`/`Rename` lines that previously lived inline in `mergeJSONServer` (old lines 42–58), since `atomicWrite` replaces them.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/setup/ -run 'TestMergeMarkerBlock|TestMerge' -v`
Expected: PASS — new marker-block tests AND the existing jsonmerge tests (the refactor is behavior-preserving).

- [ ] **Step 6: Commit**

```bash
git add internal/setup/rules.go internal/setup/rules_test.go internal/setup/jsonmerge.go
git commit -m "feat(setup): rule content + idempotent marker-block writer (shared atomicWrite)"
```

---

### Task 3: RuleWriter interface, writers, factory, and WriteRules orchestrator

**Files:**
- Modify: `internal/setup/rules.go`
- Modify: `internal/setup/rules_test.go`

- [ ] **Step 1: Write failing tests for the writers and orchestrator**

Append to `internal/setup/rules_test.go`:

```go
func TestFileRuleWriterWritesGlobalFile(t *testing.T) {
	home := t.TempDir()
	w := ruleWriterFor("claude", home)
	if w == nil {
		t.Fatal("no writer for claude")
	}
	res := w.WriteRule(false)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res.Action != "written" {
		t.Fatalf("action = %q, want written", res.Action)
	}
	want := filepath.Join(home, ".claude", "CLAUDE.md")
	if res.Path != want {
		t.Fatalf("path = %q, want %q", res.Path, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("file not written: %v", err)
	}
}

func TestRuleWriterForPaths(t *testing.T) {
	home := "/home/u"
	cases := map[string]string{
		"claude":   filepath.Join(home, ".claude", "CLAUDE.md"),
		"codex":    filepath.Join(home, ".codex", "AGENTS.md"),
		"opencode": filepath.Join(home, ".config", "opencode", "AGENTS.md"),
	}
	for name, wantPath := range cases {
		w := ruleWriterFor(name, home)
		fw, ok := w.(fileRuleWriter)
		if !ok {
			t.Fatalf("%s: not a fileRuleWriter", name)
		}
		if fw.path != wantPath {
			t.Fatalf("%s path = %q, want %q", name, fw.path, wantPath)
		}
	}
}

func TestManualRuleWriterCursor(t *testing.T) {
	w := ruleWriterFor("cursor", t.TempDir())
	res := w.WriteRule(false)
	if !res.Manual || res.Action != "manual" {
		t.Fatalf("cursor should be manual: %#v", res)
	}
	if res.Path != "" {
		t.Fatalf("cursor should write no file, got path %q", res.Path)
	}
	if !strings.Contains(res.Text, "release-and-resume") {
		t.Fatalf("paste text missing rule body: %q", res.Text)
	}
	if strings.Contains(res.Text, ruleBeginMarker) {
		t.Fatalf("paste text must not contain markers: %q", res.Text)
	}
}

func TestWriteRulesSelectsInstalledAgents(t *testing.T) {
	home := t.TempDir()
	agents := []Agent{claudeAgent{}, cursorAgent{home: home}, codexAgent{}, openCodeAgent{home: home}}
	results := WriteRules(agents, []string{"claude", "cursor"}, home, false)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Agent != "claude" || results[1].Agent != "cursor" {
		t.Fatalf("unexpected order/agents: %#v", results)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/setup/ -run 'RuleWriter|WriteRules|ManualRule|FileRule' -v`
Expected: FAIL — `undefined: ruleWriterFor`, `fileRuleWriter`, `WriteRules`.

- [ ] **Step 3: Add the types, writers, factory, and orchestrator to rules.go**

Append to `internal/setup/rules.go` (add `"path/filepath"` to the imports):

```go
// RuleResult is the outcome of installing the rule for one agent.
type RuleResult struct {
	Agent  string
	Action string // written | updated | appended | unchanged | would-write | manual
	Path   string // file written, or "" for manual
	Manual bool   // true for cursor — caller prints Text
	Text   string // marker-less paste text (manual only)
	Err    error
}

// RuleWriter installs the global release-and-resume rule for one agent.
type RuleWriter interface {
	WriteRule(dryRun bool) RuleResult
}

// fileRuleWriter writes the marker block into a global instruction file.
type fileRuleWriter struct {
	agent string
	path  string
}

func (w fileRuleWriter) WriteRule(dryRun bool) RuleResult {
	action, err := mergeMarkerBlock(w.path, dryRun)
	return RuleResult{Agent: w.agent, Action: action, Path: w.path, Err: err}
}

// manualRuleWriter writes nothing; it returns paste-ready text (cursor).
type manualRuleWriter struct{ agent string }

func (w manualRuleWriter) WriteRule(dryRun bool) RuleResult {
	return RuleResult{
		Agent:  w.agent,
		Action: "manual",
		Manual: true,
		Text:   strings.TrimSpace(releaseResumeRuleBody),
	}
}

// ruleWriterFor returns the writer for an agent, or nil if unknown.
// File paths are home-based to match the package's testable agents.
func ruleWriterFor(name, home string) RuleWriter {
	switch name {
	case "claude":
		return fileRuleWriter{agent: "claude", path: filepath.Join(home, ".claude", "CLAUDE.md")}
	case "codex":
		return fileRuleWriter{agent: "codex", path: filepath.Join(home, ".codex", "AGENTS.md")}
	case "opencode":
		return fileRuleWriter{agent: "opencode", path: filepath.Join(home, ".config", "opencode", "AGENTS.md")}
	case "cursor":
		return manualRuleWriter{agent: "cursor"}
	}
	return nil
}

// WriteRules installs the rule for each selected agent, preserving the
// order of agents. Mirrors Run; rule install is always global scope.
func WriteRules(agents []Agent, names []string, home string, dryRun bool) []RuleResult {
	selected := map[string]bool{}
	for _, n := range names {
		selected[n] = true
	}
	var out []RuleResult
	for _, a := range agents {
		if !selected[a.Name()] {
			continue
		}
		if w := ruleWriterFor(a.Name(), home); w != nil {
			out = append(out, w.WriteRule(dryRun))
		}
	}
	return out
}
```

> **Task 1 carry-over:** if Step-2 verification found opencode needs an `instructions`-array registration rather than a bare global `AGENTS.md`, adjust the opencode case here accordingly (e.g. also call `mergeJSONServer` on `~/.config/opencode/opencode.json` to add the file to `instructions`). Otherwise leave as written.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/setup/ -v`
Expected: PASS — all setup tests, new and existing.

- [ ] **Step 5: Commit**

```bash
git add internal/setup/rules.go internal/setup/rules_test.go
git commit -m "feat(setup): RuleWriter interface, file/manual writers, WriteRules orchestrator"
```

---

### Task 4: CLI wiring — flags, opt-in prompt, dry-run, output

**Files:**
- Modify: `cmd/runtimepulse/cmd_setup.go`
- Create: `cmd/runtimepulse/cmd_setup_rules_test.go`

- [ ] **Step 1: Write the failing tests for the CLI helpers**

Create `cmd/runtimepulse/cmd_setup_rules_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/setup"
)

func TestShouldWriteRules(t *testing.T) {
	cases := []struct {
		name           string
		rules, noRules bool
		interactive    bool
		promptYes      bool
		want           bool
	}{
		{"explicit --no-rules wins", true, true, true, true, false},
		{"explicit --rules", true, false, false, false, true},
		{"neither, non-interactive", false, false, false, false, false},
		{"neither, interactive yes", false, false, true, true, true},
		{"neither, interactive no", false, false, true, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldWriteRules(c.rules, c.noRules, c.interactive, func() bool { return c.promptYes })
			if got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestFormatRuleResults(t *testing.T) {
	out := formatRuleResults([]setup.RuleResult{
		{Agent: "claude", Action: "written", Path: "/h/.claude/CLAUDE.md"},
		{Agent: "opencode", Action: "unchanged", Path: "/h/.config/opencode/AGENTS.md"},
		{Agent: "cursor", Action: "manual", Manual: true, Text: "PASTE-BODY"},
	})
	if !strings.Contains(out, "claude") || !strings.Contains(out, "rules written") {
		t.Fatalf("missing written line:\n%s", out)
	}
	if !strings.Contains(out, "rules unchanged") {
		t.Fatalf("missing unchanged line:\n%s", out)
	}
	if !strings.Contains(out, "paste manually") || !strings.Contains(out, "PASTE-BODY") {
		t.Fatalf("missing cursor paste block:\n%s", out)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/runtimepulse/ -run 'ShouldWriteRules|FormatRuleResults' -v`
Expected: FAIL — `undefined: shouldWriteRules`, `formatRuleResults`.

- [ ] **Step 3: Add the pure helpers to cmd_setup.go**

Append to `cmd/runtimepulse/cmd_setup.go` (the `bufio`, `os`, `strings`, `term` imports already exist):

```go
// shouldWriteRules decides whether the rules step runs. --no-rules always
// wins; then --rules; otherwise, interactively prompt (default N), and
// non-interactively default to skip.
func shouldWriteRules(rules, noRules, interactive bool, prompt func() bool) bool {
	switch {
	case noRules:
		return false
	case rules:
		return true
	case interactive:
		return prompt()
	default:
		return false
	}
}

// formatRuleResults renders the per-agent rule lines plus, if any agent is
// manual (cursor), the paste-into-Settings block.
func formatRuleResults(results []setup.RuleResult) string {
	var b strings.Builder
	var manual *setup.RuleResult
	for i := range results {
		r := results[i]
		switch {
		case r.Err != nil:
			fmt.Fprintf(&b, "  %-10s ✗ rules: %v\n", r.Agent, r.Err)
		case r.Manual:
			fmt.Fprintf(&b, "  %-10s • rules: paste manually (see below)\n", r.Agent)
			manual = &results[i]
		case r.Action == "unchanged":
			fmt.Fprintf(&b, "  %-10s • rules unchanged (%s)\n", r.Agent, r.Path)
		case r.Action == "would-write":
			fmt.Fprintf(&b, "  %-10s ✓ rules: would write (%s)\n", r.Agent, r.Path)
		default: // written | updated | appended
			fmt.Fprintf(&b, "  %-10s ✓ rules %s (%s)\n", r.Agent, r.Action, r.Path)
		}
	}
	if manual != nil {
		b.WriteString("\ncursor — paste this into Settings → Rules → User Rules:\n\n")
		for _, ln := range strings.Split(manual.Text, "\n") {
			fmt.Fprintf(&b, "    %s\n", ln)
		}
	}
	return b.String()
}

// promptYesNo reads a single y/N answer from stdin (default N).
func promptYesNo(prompt string) bool {
	fmt.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}
```

- [ ] **Step 4: Run the helper tests to verify they pass**

Run: `go test ./cmd/runtimepulse/ -run 'ShouldWriteRules|FormatRuleResults' -v`
Expected: PASS.

- [ ] **Step 5: Wire flags and the rules step into the RunE body**

In `cmd/runtimepulse/cmd_setup.go`:

(a) Add flag vars to the `var` line at the top of `setupCmd`:
```go
	var all, yes, project, dryRun, rules, noRules bool
```

(b) Register the flags alongside the others (after the `dry-run` flag, line 121):
```go
	cmd.Flags().BoolVar(&rules, "rules", false, "also install the release-and-resume guidance into agents' global instructions")
	cmd.Flags().BoolVar(&noRules, "no-rules", false, "skip the global-instruction guidance step without prompting")
```

(c) In the `dryRun` early-return branch (lines 75–79), show the rules preview when `--rules` is set, before returning:
```go
			if dryRun {
				fmt.Printf("\n[dry-run] would register RuntimePulse (%s scope) for: %s\n",
					scope.String(), strings.Join(installed, ", "))
				if rules {
					fmt.Print("\n" + formatRuleResults(setup.WriteRules(agents, installed, home, true)))
				}
				return nil
			}
```

(d) After the registration loop's `Done.` line (line 110), add the rules step:
```go
			fmt.Println("\nDone. Restart any running agent sessions to pick up the new MCP server.")

			interactive := term.IsTerminal(int(os.Stdin.Fd()))
			if shouldWriteRules(rules, noRules, interactive, func() bool {
				return promptYesNo("\nAdd the release-and-resume guidance to your agents' global instructions? [y/N]: ")
			}) {
				fmt.Println()
				out := formatRuleResults(setup.WriteRules(agents, installed, home, false))
				fmt.Print(out)
				if strings.Contains(out, "✗ rules:") {
					failed = true
				}
			}

			if failed {
```

Note: the existing `if failed {` block stays; the snippet above inserts the rules step between the `Done.` line and that check, and may set `failed = true` on a rule-write error so the command exits non-zero.

- [ ] **Step 6: Build and run the full cmd + setup tests**

Run: `go test ./cmd/runtimepulse/ ./internal/setup/ -v`
Expected: PASS.

- [ ] **Step 7: Manual smoke of the dry-run path**

Run:
```bash
go run ./cmd/runtimepulse setup --all --rules --dry-run
```
Expected: detection table, a `[dry-run] would register …` line, then `✓ rules: would write (…)` lines for installed file agents and a cursor paste block if cursor is installed. No files written.

- [ ] **Step 8: Commit**

```bash
git add cmd/runtimepulse/cmd_setup.go cmd/runtimepulse/cmd_setup_rules_test.go
git commit -m "feat(setup): --rules/--no-rules flags + opt-in prompt for global guidance"
```

---

### Task 5: Smoke test, docs, and final gates

**Files:**
- Modify: `scripts/smoke.sh`
- Modify: `docs/usage/` setup documentation (the file that documents `runtimepulse setup`)
- Modify: `README.md` (if it documents setup flags)

- [ ] **Step 1: Find where setup is documented**

Run:
```bash
grep -rln "runtimepulse setup" docs/ README.md
grep -n "setup" scripts/smoke.sh
```
Expected: identifies the setup usage doc (likely `docs/usage/getting-started.md` or a dedicated setup page) and whether smoke already exercises setup.

- [ ] **Step 2: Add a hermetic dry-run rules assertion to smoke.sh**

In `scripts/smoke.sh`, after the build step (where `$rp` points at the built binary), add a check that does NOT touch the real home — set `HOME` to a temp dir for this one invocation. Use the existing `has` helper (capture-then-match; never `| grep -q` under pipefail):

```bash
# --- setup --rules dry-run is inert and prints the would-write preview ---
rules_out="$(HOME="$tmp/fakehome" "$rp" setup --all --rules --dry-run 2>&1 || true)"
has "$rules_out" "dry-run" || { echo "setup --rules --dry-run missing preview"; echo "$rules_out"; exit 1; }
# dry-run must not create any instruction file
if [ -e "$tmp/fakehome/.claude/CLAUDE.md" ]; then
  echo "setup --dry-run wrote a file"; exit 1
fi
echo "setup --rules dry-run OK"
```
(Use whatever temp-dir variable smoke.sh already defines — match the existing `$tmp`/`$WORK` naming. If no installed agents exist in CI, the preview still prints the `[dry-run] would register` / no-agents line; assert on `dry-run` text which is always present, OR guard the block to run only when an agent CLI is present. Keep it from failing CI where no agent CLIs exist by tolerating the no-agents path: `has "$rules_out" "dry-run" || has "$rules_out" "No target agent"`.)

- [ ] **Step 3: Run smoke to verify it still ends SMOKE OK**

Run: `./scripts/smoke.sh`
Expected: ends with `SMOKE OK`.

- [ ] **Step 4: Document the rules step**

In the setup usage doc found in Step 1, add a short section:

```markdown
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
managed, marker-delimited block that is refreshed in place on re-runs and never
disturbs your existing content. Combine with `--dry-run` to preview.
```

- [ ] **Step 5: Run all gates**

Run:
```bash
go test -race ./... && go vet ./... && gofmt -l . && GOOS=windows go build ./... && GOOS=windows go vet ./...
```
Expected: all pass; `gofmt -l .` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add scripts/smoke.sh docs/ README.md
git commit -m "test(smoke)+docs: setup rules step (dry-run assertion + usage docs)"
```

---

## Self-Review

**Spec coverage:**
- §2 (research/Cursor not writable) → Task 3 `manualRuleWriter` + Task 4 paste block. ✅
- §3 (behavior: opt-in default-N, --rules/--no-rules, dry-run) → Task 4 `shouldWriteRules`, flag wiring, dry-run branch. ✅
- §4 (rule content, marker block, cursor marker-less) → Task 2 `releaseResumeRuleBody`/`managedBlock`, Task 3 `manualRuleWriter` strips markers. ✅
- §5 (mergeMarkerBlock atomic, 4 actions) → Task 2 with full action matrix tests. ✅
- §6 (RuleWriter, file/manual writers, factory) → Task 3 (decoupled-factory refinement noted in header). ✅
- §7 (CLI flags + ✓/✗/• output + cursor paste) → Task 4 `formatRuleResults`. ✅
- §8 (tests + gates + smoke) → Tasks 2–5. ✅
- §9 (YAGNI: no remove, no project .cursor/rules) → honored; not implemented. ✅
- opencode path verification (user request) → Task 1. ✅

**Placeholder scan:** none — every code step has complete code. Task 1 intentionally has a fill-in blank for the verification *finding* (that is its deliverable, not a code placeholder), and Task 5 Steps 1/4 use grep to locate the exact doc file because the doc layout isn't known until inspected.

**Type consistency:** `mergeMarkerBlock(path string, dryRun bool) (string, error)`, `atomicWrite(path string, data []byte) error`, `RuleResult{Agent,Action,Path,Manual,Text,Err}`, `RuleWriter.WriteRule(dryRun bool) RuleResult`, `ruleWriterFor(name, home string) RuleWriter`, `WriteRules(agents []Agent, names []string, home string, dryRun bool) []RuleResult`, `shouldWriteRules(rules, noRules, interactive bool, prompt func() bool) bool`, `formatRuleResults([]setup.RuleResult) string` — consistent across all tasks.
