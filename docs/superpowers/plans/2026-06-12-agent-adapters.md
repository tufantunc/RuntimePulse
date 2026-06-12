# RuntimePulse Cursor/Codex/OpenCode Adapters Implementation Plan (Stage 5)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete spec §13 Phase 3: adapters for Cursor CLI, Codex CLI and OpenCode following the Claude pattern, so sessions registered with `agent: cursor|codex|opencode` actually resume. All four adapters share one supervised CLI runner.

**Architecture:** Extract the supervision logic from `claude.go` into a shared `runCLI` (separate stdout/stderr capture, nonzero-exit-is-Result, ctx-cancel annotation, duration, stderr-tail fallback summary) plus a shared `ParseResultJSON` (the `{"result":"..."}` shape Claude and Cursor both emit). Each adapter is then just: binary resolution (env override `RUNTIMEPULSE_<AGENT>_BIN` for hermetic tests), a pure dash-safe arg builder, and an output parser. Registry in the daemon grows to all four.

**Decisions binding this plan:**
- **No permission/sandbox flags in v1** (owner decision): no `--force`/`--yolo` (cursor), no `--sandbox`/`--full-auto` (codex), no `--dangerously-skip-permissions` (opencode). Agents run with their own configured permissions.
- **Dash-safe args** (stage-3 lesson): every prompt is positional after a `--` terminator. Cursor (commander), Codex (clap) and OpenCode (yargs) all follow the POSIX `--` convention; each adapter pins this with an args test AND a mock-binary test proving a leading-dash prompt arrives intact. Real-CLI verification stays `Validate()`'s job — the references mark several flags UNVERIFIED.
- **Working directory over directory flags:** we set `cmd.Dir = sess.RepoPath` (as Claude does) instead of `--workspace`/`-C`/`--dir` — fewer unverified flags, same effect per the references.
- **Codex output:** default exec mode writes progress to stderr and the final message to stdout — capture them separately; no `--json` in v1.
- **OpenCode caveat (documented):** its CLI has an exit-0-on-error history (`docs/reference/opencode.md` §5); v1 trusts the exit code anyway and records the output summary so failures are at least visible. The serve/HTTP path is a future improvement, not in scope.
- Per-CLI facts come from `docs/reference/cursor-cli.md`, `codex-cli.md`, `opencode.md` — consult them, don't guess.

**Tech Stack:** Go stdlib only (`os/exec`, `bytes`, `encoding/json`).

**File structure:**

```
internal/adapter/run.go            — shared runCLI + ParseResultJSON + truncate (moved)
internal/adapter/run_test.go       — runner tests (timeout annotation, stderr fallback, exit codes)
internal/adapter/claude.go         — refactored onto runCLI/ParseResultJSON (modify)
internal/adapter/cursor.go         — Cursor adapter (binary: agent → cursor-agent fallback)
internal/adapter/cursor_test.go
internal/adapter/codex.go          — Codex adapter (exec resume, stdout=final message)
internal/adapter/codex_test.go
internal/adapter/opencode.go       — OpenCode adapter (run --session)
internal/adapter/opencode_test.go
internal/daemon/daemon.go          — registry: all four adapters (modify)
scripts/smoke.sh                   — cursor-mock manual continue section (modify)
docs/PROJECT.md                    — §6 env overrides note, §13 phase-3 adapters mark (modify)
```

---

### Task 1: Shared CLI runner (refactor claude.go onto it)

**Files:**
- Create: `internal/adapter/run.go`, `internal/adapter/run_test.go`
- Modify: `internal/adapter/claude.go`

Behavior change vs the old Claude runner (intentional, covered by tests): stdout/stderr are captured separately; the parser sees stdout only; when the parsed summary is empty, the stderr tail becomes the summary (so a CLI that errors only to stderr still leaves a visible trace).

- [ ] **Step 1: Write the failing tests**

`internal/adapter/run_test.go`:

```go
package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mockBin(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mock-bin")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func rawSummary(out []byte) string { return truncate(strings.TrimSpace(string(out))) }

func TestRunCLISuccessParsesStdoutOnly(t *testing.T) {
	bin := mockBin(t, "#!/bin/sh\necho 'final answer'\necho 'progress noise' >&2\n")
	res, err := runCLI(context.Background(), bin, []string{"a"}, t.TempDir(), rawSummary)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.OutputSummary != "final answer" {
		t.Fatalf("stderr leaked into summary or wrong exit: %#v", res)
	}
	if !strings.Contains(res.Command, "mock-bin a") {
		t.Fatalf("command not recorded: %q", res.Command)
	}
}

func TestRunCLIStderrFallbackWhenStdoutEmpty(t *testing.T) {
	bin := mockBin(t, "#!/bin/sh\necho 'boom: session not found' >&2\nexit 3\n")
	res, err := runCLI(context.Background(), bin, nil, t.TempDir(), rawSummary)
	if err != nil {
		t.Fatal(err) // nonzero exit is a Result, not an error
	}
	if res.ExitCode != 3 || !strings.Contains(res.OutputSummary, "session not found") {
		t.Fatalf("stderr fallback failed: %#v", res)
	}
}

func TestRunCLITimeoutAnnotated(t *testing.T) {
	bin := mockBin(t, "#!/bin/sh\nsleep 5\n")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	res, err := runCLI(ctx, bin, nil, t.TempDir(), rawSummary)
	if err != nil {
		t.Fatalf("timeout is a Result, not an error: %v", err)
	}
	if res.ExitCode != -1 || !strings.Contains(res.OutputSummary, "timed out or cancelled") {
		t.Fatalf("timeout not annotated: %#v", res)
	}
}

func TestRunCLISpawnFailureIsError(t *testing.T) {
	if _, err := runCLI(context.Background(), "/definitely/missing/bin", nil, t.TempDir(), rawSummary); err == nil {
		t.Fatal("missing binary must be an error")
	}
}

func TestParseResultJSON(t *testing.T) {
	sum, ok := ParseResultJSON([]byte(`{"type":"result","result":"Done.","session_id":"x"}`))
	if !ok || sum != "Done." {
		t.Fatalf("json parse: %q %v", sum, ok)
	}
	sum, ok = ParseResultJSON([]byte("plain text"))
	if ok || sum != "plain text" {
		t.Fatalf("fallback: %q %v", sum, ok)
	}
}
```

Run: `go test ./internal/adapter/ -run 'TestRunCLI|TestParseResultJSON' -v` → FAIL

- [ ] **Step 2: Implement run.go**

```go
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

const summaryLimit = 500

// runCLI executes an agent CLI in dir under supervision. Contract
// (shared by all adapters): a nonzero agent exit is a Result, not an
// error; ctx cancellation/timeout is annotated in the Result; only a
// spawn failure (missing binary, …) returns an error. The parser sees
// stdout only — progress chatter on stderr never pollutes the summary,
// but becomes the fallback summary when stdout yields nothing.
func runCLI(ctx context.Context, bin string, args []string, dir string, parse func([]byte) string) (Result, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	err := cmd.Run()
	res := Result{
		Command:    bin + " " + strings.Join(args, " "),
		DurationMs: time.Since(start).Milliseconds(),
	}
	res.OutputSummary = parse(stdout.Bytes())
	if res.OutputSummary == "" {
		res.OutputSummary = truncate(strings.TrimSpace(stderr.String()))
	}

	if err != nil {
		if ctx.Err() != nil {
			res.ExitCode = -1
			res.OutputSummary = "resume timed out or cancelled: " + res.OutputSummary
			return res, nil
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// ParseResultJSON extracts the "result" field from the JSON object that
// both Claude (`--output-format json`) and Cursor (`--output-format
// json`) print; falls back to the raw text. The boolean reports whether
// structured output was recognized.
func ParseResultJSON(out []byte) (string, bool) {
	var r struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out, &r); err == nil && r.Result != "" {
		return truncate(r.Result), true
	}
	return truncate(strings.TrimSpace(string(out))), false
}
```

Move `truncate` (and the `summaryLimit` const) from claude.go into run.go unchanged (delete from claude.go).

- [ ] **Step 3: Refactor claude.go onto the runner**

`Claude.Resume` becomes:

```go
func (c Claude) Resume(ctx context.Context, sess core.Session, prompt string) (Result, error) {
	return runCLI(ctx, claudeBin(), BuildClaudeArgs(sess.SessionID, prompt), sess.RepoPath,
		func(out []byte) string { s, _ := ParseResultJSON(out); return s })
}
```

Delete `ParseClaudeOutput` and update its two tests to call `ParseResultJSON` instead (same cases — JSON, plain fallback, truncation). Keep `BuildClaudeArgs`, `claudeBin`, `Validate` as they are. All existing claude tests (dash prompt, nonzero exit, timeout, mock happy path) must still pass — they pin that the refactor preserved behavior. One expected delta: `TestClaudeResumeNonzeroExit`'s mock writes "denied" to stderr with empty stdout — under the new runner that arrives via the stderr fallback, so the assertion still holds.

- [ ] **Step 4: Run all adapter tests, verify pass**

Run: `go test -race ./internal/adapter/ -v && go test ./... > /dev/null && go vet ./... && gofmt -l .`
Expected: PASS, vet clean, gofmt empty

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/
git commit -m "refactor(adapter): shared supervised CLI runner with stdout/stderr separation"
```

---

### Task 2: Cursor adapter

**Files:**
- Create: `internal/adapter/cursor.go`
- Test: `internal/adapter/cursor_test.go`

Reference: `docs/reference/cursor-cli.md`. Binary is `agent` on current installs, `cursor-agent` on older ones — detect at call time, env `RUNTIMEPULSE_CURSOR_BIN` overrides. Resume shape: `agent -p --resume <id> --output-format json -- <prompt>` (the `-p`+`--resume` combination is UNVERIFIED in official docs — `Validate()` and the doc comment carry that caveat).

- [ ] **Step 1: Write the failing tests**

```go
package adapter

import (
	"context"
	"strings"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestBuildCursorArgs(t *testing.T) {
	args := BuildCursorArgs("chat-1", "Continue.")
	want := []string{"-p", "--resume", "chat-1", "--output-format", "json", "--", "Continue."}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestCursorResumeWithMockBinary(t *testing.T) {
	// echo the LAST argument back as the result — proves the dash-safe
	// prompt position in one test.
	bin := mockBin(t, "#!/bin/sh\nfor last; do :; done\nprintf '{\"type\":\"result\",\"result\":\"%s\"}' \"$last\"\n")
	t.Setenv("RUNTIMEPULSE_CURSOR_BIN", bin)

	c := Cursor{}
	if err := c.Validate(); err != nil {
		t.Fatalf("validate with mock bin: %v", err)
	}
	res, err := c.Resume(context.Background(),
		core.Session{SessionID: "chat-1", RepoPath: t.TempDir()}, "- dash bullet prompt")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.OutputSummary != "- dash bullet prompt" {
		t.Fatalf("dash prompt mangled or wrong exit: %#v", res)
	}
	if !strings.Contains(res.Command, "--resume chat-1") {
		t.Fatalf("command not recorded: %q", res.Command)
	}
}

func TestCursorBinFallsBackWithoutEnv(t *testing.T) {
	t.Setenv("RUNTIMEPULSE_CURSOR_BIN", "")
	bin := cursorBin()
	if bin != "agent" && bin != "cursor-agent" {
		t.Fatalf("unexpected cursor binary resolution: %q", bin)
	}
}
```

Run: `go test ./internal/adapter/ -run TestBuildCursorArgs -v` → FAIL

- [ ] **Step 2: Implement**

`internal/adapter/cursor.go`:

```go
package adapter

import (
	"context"
	"os"
	"os/exec"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// Cursor resumes Cursor CLI sessions headlessly:
//
//	agent -p --resume <chat-id> --output-format json -- "<prompt>"
//
// run in the session's repoPath (the CLI treats cwd as the repository
// root — see docs/reference/cursor-cli.md). The -p+--resume combination
// is not shown combined in official docs; Validate() existing is why.
// v1 passes NO permission flags (no --force/--trust — owner decision):
// in print mode the agent proposes edits without applying them unless
// the workspace allows it.
type Cursor struct{}

func (Cursor) Name() string { return "cursor" }

func (Cursor) Validate() error {
	_, err := exec.LookPath(cursorBin())
	return err
}

// cursorBin resolves the binary: env override → current name "agent" →
// legacy "cursor-agent" (pre-rename installs).
func cursorBin() string {
	if b := os.Getenv("RUNTIMEPULSE_CURSOR_BIN"); b != "" {
		return b
	}
	if _, err := exec.LookPath("agent"); err == nil {
		return "agent"
	}
	return "cursor-agent"
}

// BuildCursorArgs: the prompt is positional after "--" so dash-leading
// prompts can never be eaten by the option parser (stage-3 lesson).
func BuildCursorArgs(sessionID, prompt string) []string {
	return []string{"-p", "--resume", sessionID, "--output-format", "json", "--", prompt}
}

func (c Cursor) Resume(ctx context.Context, sess core.Session, prompt string) (Result, error) {
	return runCLI(ctx, cursorBin(), BuildCursorArgs(sess.SessionID, prompt), sess.RepoPath,
		func(out []byte) string { s, _ := ParseResultJSON(out); return s })
}
```

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/adapter/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/
git commit -m "feat(adapter): cursor cli adapter with binary fallback and dash-safe args"
```

---

### Task 3: Codex adapter

**Files:**
- Create: `internal/adapter/codex.go`
- Test: `internal/adapter/codex_test.go`

Reference: `docs/reference/codex-cli.md`. Resume: `codex exec resume <id> -- <prompt>`; default exec mode writes progress to stderr and ONLY the final agent message to stdout — exactly what the shared runner separates. `--skip-git-repo-check` keeps non-git repos working (operational, not a permission flag). No sandbox flags in v1 (owner decision); exec defaults to read-only sandbox — documented limitation.

- [ ] **Step 1: Write the failing tests**

```go
package adapter

import (
	"context"
	"strings"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestBuildCodexArgs(t *testing.T) {
	args := BuildCodexArgs("sess-9", "Continue.")
	want := []string{"exec", "resume", "sess-9", "--skip-git-repo-check", "--", "Continue."}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestCodexResumeSeparatesProgressFromResult(t *testing.T) {
	// codex semantics: progress → stderr, final message → stdout
	bin := mockBin(t, "#!/bin/sh\necho 'thinking...' >&2\nfor last; do :; done\necho \"did: $last\"\n")
	t.Setenv("RUNTIMEPULSE_CODEX_BIN", bin)

	c := Codex{}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	res, err := c.Resume(context.Background(),
		core.Session{SessionID: "sess-9", RepoPath: t.TempDir()}, "--dash prompt")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.OutputSummary != "did: --dash prompt" {
		t.Fatalf("wrong summary (progress leaked, or dash prompt eaten): %#v", res)
	}
	if strings.Contains(res.OutputSummary, "thinking") {
		t.Fatal("stderr progress must not pollute the summary")
	}
}
```

Run: `go test ./internal/adapter/ -run TestBuildCodexArgs -v` → FAIL

- [ ] **Step 2: Implement**

`internal/adapter/codex.go`:

```go
package adapter

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// Codex resumes Codex CLI sessions non-interactively:
//
//	codex exec resume <session-id> --skip-git-repo-check -- "<prompt>"
//
// run in the session's repoPath. Default exec mode streams progress to
// stderr and prints only the final agent message to stdout (see
// docs/reference/codex-cli.md) — the shared runner keeps them apart.
// v1 passes NO sandbox/approval flags (owner decision); codex exec
// defaults to a read-only sandbox, a documented limitation.
type Codex struct{}

func (Codex) Name() string { return "codex" }

func (Codex) Validate() error {
	_, err := exec.LookPath(codexBin())
	return err
}

func codexBin() string {
	if b := os.Getenv("RUNTIMEPULSE_CODEX_BIN"); b != "" {
		return b
	}
	return "codex"
}

// BuildCodexArgs: prompt positional after "--" (dash-safe, stage-3
// lesson); --skip-git-repo-check keeps non-git repoPaths working.
func BuildCodexArgs(sessionID, prompt string) []string {
	return []string{"exec", "resume", sessionID, "--skip-git-repo-check", "--", prompt}
}

func (c Codex) Resume(ctx context.Context, sess core.Session, prompt string) (Result, error) {
	return runCLI(ctx, codexBin(), BuildCodexArgs(sess.SessionID, prompt), sess.RepoPath,
		func(out []byte) string { return truncate(strings.TrimSpace(string(out))) })
}
```

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/adapter/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/
git commit -m "feat(adapter): codex cli adapter with stdout/stderr separation"
```

---

### Task 4: OpenCode adapter

**Files:**
- Create: `internal/adapter/opencode.go`
- Test: `internal/adapter/opencode_test.go`

Reference: `docs/reference/opencode.md`. Resume: `opencode run --session <id> -- <prompt>` in the session's repoPath (sessions are stored per-project keyed by directory). Known caveat: opencode's CLI has an exit-0-on-error history — v1 trusts the exit code and records the summary; the serve/HTTP path is the future fix.

- [ ] **Step 1: Write the failing tests**

```go
package adapter

import (
	"context"
	"testing"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

func TestBuildOpenCodeArgs(t *testing.T) {
	args := BuildOpenCodeArgs("ses_abc", "Continue.")
	want := []string{"run", "--session", "ses_abc", "--", "Continue."}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestOpenCodeResumeWithMockBinary(t *testing.T) {
	bin := mockBin(t, "#!/bin/sh\nfor last; do :; done\necho \"oc: $last\"\n")
	t.Setenv("RUNTIMEPULSE_OPENCODE_BIN", bin)

	o := OpenCode{}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	res, err := o.Resume(context.Background(),
		core.Session{SessionID: "ses_abc", RepoPath: t.TempDir()}, "-leading dash")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.OutputSummary != "oc: -leading dash" {
		t.Fatalf("dash prompt mangled or wrong exit: %#v", res)
	}
}
```

Run: `go test ./internal/adapter/ -run TestBuildOpenCodeArgs -v` → FAIL

- [ ] **Step 2: Implement**

`internal/adapter/opencode.go`:

```go
package adapter

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

// OpenCode resumes OpenCode sessions non-interactively:
//
//	opencode run --session <ses_id> -- "<prompt>"
//
// run in the session's repoPath (opencode stores sessions per-project
// keyed by directory — see docs/reference/opencode.md). Known caveat,
// documented there: opencode's CLI historically exits 0 on some errors;
// v1 trusts the exit code and records the output summary so failures
// are at least visible. The serve/HTTP path is a future improvement.
// v1 passes NO permission flags (owner decision).
type OpenCode struct{}

func (OpenCode) Name() string { return "opencode" }

func (OpenCode) Validate() error {
	_, err := exec.LookPath(openCodeBin())
	return err
}

func openCodeBin() string {
	if b := os.Getenv("RUNTIMEPULSE_OPENCODE_BIN"); b != "" {
		return b
	}
	return "opencode"
}

// BuildOpenCodeArgs: prompt positional after "--" (dash-safe).
func BuildOpenCodeArgs(sessionID, prompt string) []string {
	return []string{"run", "--session", sessionID, "--", prompt}
}

func (o OpenCode) Resume(ctx context.Context, sess core.Session, prompt string) (Result, error) {
	return runCLI(ctx, openCodeBin(), BuildOpenCodeArgs(sess.SessionID, prompt), sess.RepoPath,
		func(out []byte) string { return truncate(strings.TrimSpace(string(out))) })
}
```

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/adapter/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/
git commit -m "feat(adapter): opencode cli adapter"
```

---

### Task 5: Registry wiring + smoke + docs

**Files:**
- Modify: `internal/daemon/daemon.go` (registry), `internal/daemon/dispatch_rpc_test.go` (append), `scripts/smoke.sh`, `docs/PROJECT.md`

- [ ] **Step 1: Write the failing test (append to dispatch_rpc_test.go)**

```go
func TestCursorSessionDispatchesViaMock(t *testing.T) {
	dir := shortTempDir(t)
	mock := filepath.Join(dir, "mock-agent")
	if err := os.WriteFile(mock,
		[]byte("#!/bin/sh\nprintf '{\"type\":\"result\",\"result\":\"cursor turn done\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIMEPULSE_CURSOR_BIN", mock)
	_, c := startTestDaemon(t)

	if err := c.Call("session.register",
		map[string]any{"sessionId": "cur-1", "agent": "cursor", "repoPath": "/tmp"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Call("continuation.run",
		map[string]any{"sessionId": "cur-1", "prompt": "go"}, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var done []map[string]any
		if err := c.Call("continuation.list", map[string]any{"state": "completed"}, &done); err != nil {
			t.Fatal(err)
		}
		if len(done) == 1 {
			if s, _ := done[0]["outputSummary"].(string); s != "cursor turn done" {
				t.Fatalf("summary = %v", done[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cursor continuation never completed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
```

Run: `go test ./internal/daemon/ -run TestCursorSession -v` → FAIL (no cursor adapter in registry → continuation fails with "no adapter")

- [ ] **Step 2: Wire the registry**

In `internal/daemon/daemon.go`, the dispatcher construction becomes:

```go
	disp := dispatch.New(st, adapter.Registry{
		"claude":   adapter.Claude{},
		"cursor":   adapter.Cursor{},
		"codex":    adapter.Codex{},
		"opencode": adapter.OpenCode{},
	}, eng.Ingest)
```

(The unknown-agent error message already enumerates registry keys dynamically — no other change.)

- [ ] **Step 3: Smoke — cursor-mock section**

In `scripts/smoke.sh`, next to the mock-claude install at the top, add a mock cursor binary:

```bash
cat > "$dir/mock-cursor" << 'MOCK'
#!/bin/sh
printf '{"type":"result","result":"cursor mock done"}'
MOCK
chmod +x "$dir/mock-cursor"
export RUNTIMEPULSE_CURSOR_BIN="$dir/mock-cursor"
```

And before the MCP section, add (REMEMBER the smoke SIGPIPE rule — use the `has` helper, never `| grep -q`):

```bash
# --- multi-agent: a cursor session dispatches via its own adapter ---
"$rp" session register --agent cursor --session smoke-cur --repo "$dir"
"$rp" continue --session smoke-cur --prompt "cursor poke"
curdone=""
for _ in $(seq 1 25); do
  if has 'cursor mock done' "$rp" continuations --state completed; then curdone=1; break; fi
  sleep 0.2
done
[ -n "$curdone" ] || { echo "FAIL: cursor continuation missing"; exit 1; }
```

- [ ] **Step 4: Docs**

`docs/PROJECT.md`:
- §6 adapter table: add a note line — "Adapter binaries are overridable via `RUNTIMEPULSE_CLAUDE_BIN` / `RUNTIMEPULSE_CURSOR_BIN` / `RUNTIMEPULSE_CODEX_BIN` / `RUNTIMEPULSE_OPENCODE_BIN` (hermetic tests). All prompts pass positionally after a `--` terminator. v1 passes no permission/sandbox flags for any agent."
- §13 item 3: the Cursor/Codex/OpenCode part is now done — adjust the line so the whole item reads as completed.

- [ ] **Step 5: Full verification**

Run: `go test -race ./... && go vet ./... && gofmt -l .` then `./scripts/smoke.sh` ×3 — all `SMOKE OK`.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/ scripts/smoke.sh docs/PROJECT.md
git commit -m "feat(daemon): register cursor/codex/opencode adapters; multi-agent smoke"
```

---

## Self-Review Notes

- **Spec coverage:** §13 Phase 3 adapter items → Tasks 2–4; §6 adapter interface honored (Name/Resume/Validate); resume commands match `docs/reference/*` with the deliberate cmd.Dir-instead-of-dir-flags decision documented per adapter.
- **Type consistency:** all three new adapters implement the existing `Adapter` interface; `runCLI`/`ParseResultJSON`/`truncate` signatures used identically across claude/cursor/codex/opencode; registry literal matches type names.
- **Stage-3 lessons honored:** `--` terminator everywhere with both args-builder and mock-binary dash tests; `RUNTIMEPULSE_<AGENT>_BIN` env pattern; no permission flags; ctx-cancel annotation preserved in the shared runner; smoke additions use `has` (never `| grep -q` under pipefail).
- **Refactor risk:** Task 1 changes Claude's capture from CombinedOutput to split streams; existing claude tests pin the visible behavior (the nonzero-exit "denied"-on-stderr case now flows through the stderr fallback and still passes).
```
