# RuntimePulse MCP Setup Wizard Implementation Plan (Stage 9)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `runtimepulse setup` detects installed agent CLIs (Claude, Cursor, Codex, OpenCode) and registers RuntimePulse as their MCP server in one step — native `mcp add` for Claude/Codex, safe JSON merge for Cursor/OpenCode.

**Architecture:** A new `internal/setup` package defines an `Agent` interface with four implementations split into two families: native-CLI agents (subprocess via an injectable `runCmd` seam) and JSON-file agents (a shared atomic merge helper). A thin `cmd/runtimepulse/cmd_setup.go` resolves `os.Executable()`, detects, prompts/honors flags, registers, and reports. All write behavior lives in the testable package.

**Tech Stack:** Go stdlib only (`os/exec`, `encoding/json`, `os`, `path/filepath`), existing `internal/mockexe` for native-agent tests, cobra.

**Spec:** `docs/superpowers/specs/2026-06-12-mcp-setup-wizard-design.md` (approved). Verified facts: only Claude and Codex have `mcp add`; Cursor (`~/.cursor/mcp.json`) and OpenCode (config JSON) have none. We never hand-edit Codex's TOML.

**File structure:**

```
internal/setup/setup.go          — Scope, Status, Agent interface, Registry(), DetectAll, Run, Result
internal/setup/jsonmerge.go      — mergeJSONServer: atomic read-merge-write of a nested JSON map
internal/setup/jsonmerge_test.go
internal/setup/native.go         — claudeAgent, codexAgent (runCmd seam)
internal/setup/native_test.go
internal/setup/file.go           — cursorAgent, openCodeAgent (paths + merge)
internal/setup/file_test.go
internal/setup/setup_test.go     — DetectAll/Run orchestration
cmd/runtimepulse/cmd_setup.go    — the `setup` command
cmd/runtimepulse/main.go         — register setupCmd (modify)
scripts/smoke.sh                 — setup --all dry-run + register section (modify)
README.md, docs/usage/mcp.md, docs/PROJECT.md — docs (modify)
```

Build order: jsonmerge (Task 1) → file agents (Task 2) → native agents (Task 3) → orchestration (Task 4) → CLI (Task 5) → smoke + docs (Task 6). Each task's tests depend only on earlier tasks.

---

### Task 1: `jsonmerge` — atomic nested-key merge

**Files:**
- Create: `internal/setup/jsonmerge.go`, `internal/setup/jsonmerge_test.go`

This is the riskiest unit: it must preserve every existing key (including `$schema` and other MCP servers), create the file+parents when absent, write atomically, and refuse to clobber an unparseable file.

- [ ] **Step 1: Write the failing tests**

`internal/setup/jsonmerge_test.go`:

```go
package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, b)
	}
	return m
}

func TestMergeCreatesFileAndParents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "mcp.json")
	entry := map[string]any{"command": "/bin/rp", "args": []string{"mcp"}}
	if err := mergeJSONServer(path, "mcpServers", "runtimepulse", entry); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, path)
	servers := m["mcpServers"].(map[string]any)
	rp := servers["runtimepulse"].(map[string]any)
	if rp["command"] != "/bin/rp" {
		t.Fatalf("entry not written: %#v", rp)
	}
}

func TestMergePreservesExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	seed := `{"$schema":"https://x/config.json","mcpServers":{"other":{"command":"keep"}}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{"command": "/bin/rp", "args": []string{"mcp"}}
	if err := mergeJSONServer(path, "mcpServers", "runtimepulse", entry); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, path)
	if m["$schema"] != "https://x/config.json" {
		t.Fatal("$schema lost")
	}
	servers := m["mcpServers"].(map[string]any)
	if servers["other"].(map[string]any)["command"] != "keep" {
		t.Fatal("existing server lost")
	}
	if servers["runtimepulse"] == nil {
		t.Fatal("runtimepulse not added")
	}
}

func TestMergeIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	entry := map[string]any{"command": "/bin/rp", "args": []string{"mcp"}}
	for i := 0; i < 2; i++ {
		if err := mergeJSONServer(path, "mcp", "runtimepulse", entry); err != nil {
			t.Fatal(err)
		}
	}
	m := readJSON(t, path)
	mcp := m["mcp"].(map[string]any)
	if len(mcp) != 1 {
		t.Fatalf("re-run duplicated keys: %#v", mcp)
	}
}

func TestMergeRefusesUnparseableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeJSONServer(path, "mcp", "runtimepulse", map[string]any{}); err == nil {
		t.Fatal("must refuse to overwrite an unparseable file")
	}
	// original untouched
	b, _ := os.ReadFile(path)
	if string(b) != "{ not json" {
		t.Fatalf("clobbered the bad file: %s", b)
	}
}

func TestHasServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if hasJSONServer(path, "mcp", "runtimepulse") {
		t.Fatal("absent file must report not-registered")
	}
	mergeJSONServer(path, "mcp", "runtimepulse", map[string]any{"command": "x"})
	if !hasJSONServer(path, "mcp", "runtimepulse") {
		t.Fatal("registered server must be detected")
	}
}
```

Run: `go test ./internal/setup/ -v` → FAIL (package missing)

- [ ] **Step 2: Implement**

`internal/setup/jsonmerge.go`:

```go
// Package setup registers RuntimePulse as an MCP server with the agent
// CLIs installed on the machine (the `runtimepulse setup` wizard).
package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// mergeJSONServer sets config[section][name] = entry in the JSON file at
// path, preserving every other key, creating the file and parent dirs
// when absent, and writing atomically. It refuses to touch a file that
// is not valid JSON (never clobber what we can't understand).
func mergeJSONServer(path, section, name string, entry map[string]any) error {
	root := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &root); err != nil {
			return fmt.Errorf("setup: %s is not valid JSON, leaving it untouched: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	sec, ok := root[section].(map[string]any)
	if !ok {
		sec = map[string]any{}
		root[section] = sec
	}
	sec[name] = entry

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// hasJSONServer reports whether config[section][name] already exists.
func hasJSONServer(path, section, name string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var root map[string]any
	if json.Unmarshal(b, &root) != nil {
		return false
	}
	sec, ok := root[section].(map[string]any)
	if !ok {
		return false
	}
	_, ok = sec[name]
	return ok
}
```

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/setup/ -v && go vet ./... && gofmt -l .`
Expected: PASS, clean
Windows gate: `GOOS=windows go build ./...`

- [ ] **Step 4: Commit**

```bash
git add internal/setup/
git commit -m "feat(setup): atomic nested-key JSON merge helper"
```

---

### Task 2: File agents — Cursor, OpenCode

**Files:**
- Create: `internal/setup/setup.go` (types only, this task), `internal/setup/file.go`, `internal/setup/file_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/setup/file_test.go`:

```go
package setup

import (
	"path/filepath"
	"testing"
)

func TestCursorRegisterUserScope(t *testing.T) {
	home := t.TempDir()
	a := cursorAgent{home: home, cwd: t.TempDir()}
	if !a.SupportsScope(ScopeProject) || !a.SupportsScope(ScopeUser) {
		t.Fatal("cursor supports both scopes")
	}
	if err := a.Register("/abs/runtimepulse", ScopeUser); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".cursor", "mcp.json")
	m := readJSON(t, path)
	rp := m["mcpServers"].(map[string]any)["runtimepulse"].(map[string]any)
	if rp["command"] != "/abs/runtimepulse" {
		t.Fatalf("bad command: %#v", rp)
	}
	args := rp["args"].([]any)
	if len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("bad args: %#v", args)
	}
	if !a.Status(ScopeUser).Registered {
		t.Fatal("Status must report registered after Register")
	}
}

func TestCursorRegisterProjectScope(t *testing.T) {
	cwd := t.TempDir()
	a := cursorAgent{home: t.TempDir(), cwd: cwd}
	if err := a.Register("/abs/runtimepulse", ScopeProject); err != nil {
		t.Fatal(err)
	}
	readJSON(t, filepath.Join(cwd, ".cursor", "mcp.json")) // exists & valid
}

func TestOpenCodeRegisterShape(t *testing.T) {
	cwd := t.TempDir()
	a := openCodeAgent{home: t.TempDir(), cwd: cwd}
	if err := a.Register("/abs/runtimepulse", ScopeProject); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, filepath.Join(cwd, "opencode.json"))
	rp := m["mcp"].(map[string]any)["runtimepulse"].(map[string]any)
	if rp["type"] != "local" || rp["enabled"] != true {
		t.Fatalf("bad opencode entry: %#v", rp)
	}
	cmd := rp["command"].([]any)
	if len(cmd) != 2 || cmd[0] != "/abs/runtimepulse" || cmd[1] != "mcp" {
		t.Fatalf("bad command array: %#v", cmd)
	}
}
```

Run: `go test ./internal/setup/ -run 'TestCursor|TestOpenCode' -v` → FAIL

- [ ] **Step 2: Implement the shared types**

`internal/setup/setup.go` (types only for now; orchestration lands in Task 4):

```go
package setup

// Scope selects user/global vs current-project registration.
type Scope int

const (
	ScopeUser Scope = iota
	ScopeProject
)

func (s Scope) String() string {
	if s == ScopeProject {
		return "project"
	}
	return "user"
}

// Status is the result of inspecting one agent.
type Status struct {
	Installed  bool
	Registered bool
	Detail     string
}

// Agent registers RuntimePulse as an MCP server for one agent CLI.
type Agent interface {
	Name() string
	Status(scope Scope) Status
	Register(binPath string, scope Scope) error
	SupportsScope(scope Scope) bool
}
```

- [ ] **Step 3: Implement the file agents**

`internal/setup/file.go`:

```go
package setup

import (
	"os/exec"
	"path/filepath"
)

// --- Cursor: ~/.cursor/mcp.json (user) or <cwd>/.cursor/mcp.json (project) ---

type cursorAgent struct {
	home string // injected in tests; production uses os.UserHomeDir
	cwd  string
}

func (cursorAgent) Name() string { return "cursor" }

func (cursorAgent) SupportsScope(Scope) bool { return true }

func (a cursorAgent) installed() bool {
	if _, err := exec.LookPath("agent"); err == nil {
		return true
	}
	_, err := exec.LookPath("cursor-agent") // pre-rename installs
	return err == nil
}

func (a cursorAgent) path(scope Scope) string {
	if scope == ScopeProject {
		return filepath.Join(a.cwd, ".cursor", "mcp.json")
	}
	return filepath.Join(a.home, ".cursor", "mcp.json")
}

func (a cursorAgent) Status(scope Scope) Status {
	return Status{
		Installed:  a.installed(),
		Registered: hasJSONServer(a.path(scope), "mcpServers", "runtimepulse"),
	}
}

func (a cursorAgent) Register(binPath string, scope Scope) error {
	return mergeJSONServer(a.path(scope), "mcpServers", "runtimepulse",
		map[string]any{"command": binPath, "args": []string{"mcp"}})
}

// --- OpenCode: global config (user) or <cwd>/opencode.json (project) ---

type openCodeAgent struct {
	home string
	cwd  string
}

func (openCodeAgent) Name() string { return "opencode" }

func (openCodeAgent) SupportsScope(Scope) bool { return true }

func (a openCodeAgent) installed() bool {
	_, err := exec.LookPath("opencode")
	return err == nil
}

func (a openCodeAgent) path(scope Scope) string {
	if scope == ScopeProject {
		return filepath.Join(a.cwd, "opencode.json")
	}
	// VERIFY during implementation against opencode docs; ~/.config/opencode
	// is the documented XDG location on macOS/Linux.
	return filepath.Join(a.home, ".config", "opencode", "opencode.json")
}

func (a openCodeAgent) Status(scope Scope) Status {
	return Status{
		Installed:  a.installed(),
		Registered: hasJSONServer(a.path(scope), "mcp", "runtimepulse"),
	}
}

func (a openCodeAgent) Register(binPath string, scope Scope) error {
	return mergeJSONServer(a.path(scope), "mcp", "runtimepulse",
		map[string]any{"type": "local", "command": []string{binPath, "mcp"}, "enabled": true})
}
```

> **Implementation note:** confirm OpenCode's global config path against current docs (`opencode` "config" page). If it differs from `~/.config/opencode/opencode.json`, fix `openCodeAgent.path` and the spec's UX example — keep the project path `<cwd>/opencode.json` (that one is documented). The `home`/`cwd` fields exist precisely so tests pin behavior regardless of the real location.

- [ ] **Step 4: Run tests, verify pass**

Run: `go test -race ./internal/setup/ -v && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/setup/
git commit -m "feat(setup): cursor and opencode agents via JSON merge"
```

---

### Task 3: Native agents — Claude, Codex

**Files:**
- Create: `internal/setup/native.go`, `internal/setup/native_test.go`

`Register` shells out to the agent's own `mcp add`. The subprocess call goes through an injectable `runCmd` field so tests can record argv without a real agent CLI.

- [ ] **Step 1: Write the failing tests**

`internal/setup/native_test.go`:

```go
package setup

import (
	"strings"
	"testing"
)

// recordCmd captures the most recent command for assertions.
type recordCmd struct{ name string; args []string; err error }

func (r *recordCmd) run(name string, args ...string) error {
	r.name, r.args = name, args
	return r.err
}

func TestClaudeRegisterUserScope(t *testing.T) {
	rec := &recordCmd{}
	a := claudeAgent{run: rec.run}
	if err := a.Register("/abs/runtimepulse", ScopeUser); err != nil {
		t.Fatal(err)
	}
	got := rec.name + " " + strings.Join(rec.args, " ")
	want := "claude mcp add --transport stdio --scope user runtimepulse -- /abs/runtimepulse mcp"
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func TestClaudeRegisterProjectScope(t *testing.T) {
	rec := &recordCmd{}
	a := claudeAgent{run: rec.run}
	a.Register("/abs/runtimepulse", ScopeProject)
	if !contains(rec.args, "--scope") || !contains(rec.args, "project") {
		t.Fatalf("project scope not passed: %v", rec.args)
	}
}

func TestCodexRegisterArgsAndScope(t *testing.T) {
	rec := &recordCmd{}
	a := codexAgent{run: rec.run}
	if a.SupportsScope(ScopeProject) {
		t.Fatal("codex must not advertise project scope in v1")
	}
	if err := a.Register("/abs/runtimepulse", ScopeUser); err != nil {
		t.Fatal(err)
	}
	got := rec.name + " " + strings.Join(rec.args, " ")
	want := "codex mcp add runtimepulse -- /abs/runtimepulse mcp"
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
```

Run: `go test ./internal/setup/ -run 'TestClaude|TestCodex' -v` → FAIL

- [ ] **Step 2: Implement**

`internal/setup/native.go`:

```go
package setup

import (
	"bytes"
	"os/exec"
	"strings"
)

// runFunc runs an agent CLI subcommand; injectable for tests.
type runFunc func(name string, args ...string) error

// execRun is the production runFunc.
func execRun(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

// queryFunc returns an agent CLI subcommand's combined output; injectable.
type queryFunc func(name string, args ...string) (string, error)

func execQuery(name string, args ...string) (string, error) {
	var buf bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}

// --- Claude: `claude mcp add` (both scopes) ---

type claudeAgent struct {
	run   runFunc   // nil → execRun
	query queryFunc // nil → execQuery
}

func (claudeAgent) Name() string { return "claude" }

func (claudeAgent) SupportsScope(Scope) bool { return true }

func (a claudeAgent) installed() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

func (a claudeAgent) Status(scope Scope) Status {
	st := Status{Installed: a.installed()}
	if !st.Installed {
		return st
	}
	q := a.query
	if q == nil {
		q = execQuery
	}
	out, err := q("claude", "mcp", "list")
	st.Registered = err == nil && strings.Contains(out, "runtimepulse")
	return st
}

func (a claudeAgent) Register(binPath string, scope Scope) error {
	run := a.run
	if run == nil {
		run = execRun
	}
	scopeName := "user"
	if scope == ScopeProject {
		scopeName = "project"
	}
	return run("claude", "mcp", "add", "--transport", "stdio", "--scope", scopeName,
		"runtimepulse", "--", binPath, "mcp")
}

// --- Codex: `codex mcp add` (global only in v1) ---

type codexAgent struct {
	run   runFunc
	query queryFunc
}

func (codexAgent) Name() string { return "codex" }

// SupportsScope: codex's project config is TOML; v1 delegates entirely
// to `codex mcp add` (global), so project scope is unsupported.
func (codexAgent) SupportsScope(scope Scope) bool { return scope == ScopeUser }

func (a codexAgent) installed() bool {
	_, err := exec.LookPath("codex")
	return err == nil
}

func (a codexAgent) Status(scope Scope) Status {
	st := Status{Installed: a.installed()}
	if !st.Installed {
		return st
	}
	q := a.query
	if q == nil {
		q = execQuery
	}
	out, err := q("codex", "mcp", "list")
	st.Registered = err == nil && strings.Contains(out, "runtimepulse")
	return st
}

func (a codexAgent) Register(binPath string, scope Scope) error {
	run := a.run
	if run == nil {
		run = execRun
	}
	return run("codex", "mcp", "add", "runtimepulse", "--", binPath, "mcp")
}
```

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/setup/ -v && go vet ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/setup/
git commit -m "feat(setup): claude and codex agents via native mcp add"
```

---

### Task 4: Orchestration — `DetectAll` and `Run`

**Files:**
- Modify: `internal/setup/setup.go`
- Create/extend: `internal/setup/setup_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/setup/setup_test.go`:

```go
package setup

import "testing"

// fakeAgent lets the orchestration tests avoid real CLIs/filesystem.
type fakeAgent struct {
	name       string
	installed  bool
	registered bool
	supportPrj bool
	regErr     error
	regCalls   int
}

func (f *fakeAgent) Name() string { return f.name }
func (f *fakeAgent) Status(Scope) Status {
	return Status{Installed: f.installed, Registered: f.registered}
}
func (f *fakeAgent) SupportsScope(s Scope) bool { return s == ScopeUser || f.supportPrj }
func (f *fakeAgent) Register(string, Scope) error {
	f.regCalls++
	return f.regErr
}

func TestRunRegistersOnlySelectedInstalled(t *testing.T) {
	claude := &fakeAgent{name: "claude", installed: true}
	cursor := &fakeAgent{name: "cursor", installed: false}
	agents := []Agent{claude, cursor}

	results := Run(agents, []string{"claude", "cursor"}, "/bin/rp", ScopeUser)

	if claude.regCalls != 1 {
		t.Fatalf("installed selected agent must be registered once, got %d", claude.regCalls)
	}
	if cursor.regCalls != 0 {
		t.Fatal("uninstalled agent must not be registered")
	}
	if len(results) != 2 {
		t.Fatalf("a result per selected agent, got %d", len(results))
	}
}

func TestRunReportsFailureIndependently(t *testing.T) {
	ok := &fakeAgent{name: "claude", installed: true}
	bad := &fakeAgent{name: "opencode", installed: true, regErr: errBoom}
	results := Run([]Agent{ok, bad}, []string{"claude", "opencode"}, "/bin/rp", ScopeUser)

	var okRes, badRes *Result
	for i := range results {
		switch results[i].Agent {
		case "claude":
			okRes = &results[i]
		case "opencode":
			badRes = &results[i]
		}
	}
	if okRes == nil || !okRes.OK {
		t.Fatal("good agent must succeed despite the other's failure")
	}
	if badRes == nil || badRes.OK || badRes.Err == nil {
		t.Fatal("failing agent must be reported, not abort the run")
	}
}

func TestRunSkipsAlreadyRegistered(t *testing.T) {
	a := &fakeAgent{name: "claude", installed: true, registered: true}
	results := Run([]Agent{a}, []string{"claude"}, "/bin/rp", ScopeUser)
	if a.regCalls != 0 {
		t.Fatal("already-registered agent must not be re-registered")
	}
	if !results[0].Skipped {
		t.Fatal("already-registered must be reported as skipped")
	}
}

func TestRunUnsupportedScopeFallsBackToUser(t *testing.T) {
	codex := &fakeAgent{name: "codex", installed: true, supportPrj: false}
	results := Run([]Agent{codex}, []string{"codex"}, "/bin/rp", ScopeProject)
	if codex.regCalls != 1 {
		t.Fatal("codex must still register at user scope")
	}
	if results[0].Note == "" {
		t.Fatal("a note must explain the scope fallback")
	}
}

var errBoom = &boomErr{}

type boomErr struct{}

func (*boomErr) Error() string { return "boom" }
```

Run: `go test ./internal/setup/ -run TestRun -v` → FAIL (Run/Result undefined)

- [ ] **Step 2: Implement (append to setup.go)**

```go
import "os"

// Result is the outcome of registering one agent.
type Result struct {
	Agent   string
	OK      bool
	Skipped bool   // already registered
	Note    string // e.g. scope fallback explanation
	Err     error
}

// AgentStatus pairs an agent with its detected status (for the wizard's
// detection table).
type AgentStatus struct {
	Agent  Agent
	Status Status
}

// Registry returns the four supported agents, wired for production
// (real home/cwd, real subprocess runner). home and cwd come from the
// environment; a caller may pass overrides for testing.
func Registry(home, cwd string) []Agent {
	return []Agent{
		claudeAgent{},
		cursorAgent{home: home, cwd: cwd},
		codexAgent{},
		openCodeAgent{home: home, cwd: cwd},
	}
}

// DetectAll inspects every agent at the given scope.
func DetectAll(agents []Agent, scope Scope) []AgentStatus {
	out := make([]AgentStatus, 0, len(agents))
	for _, a := range agents {
		out = append(out, AgentStatus{Agent: a, Status: a.Status(scope)})
	}
	return out
}

// Run registers the named, installed agents at scope. Each agent is
// independent: a failure is recorded, never fatal. Already-registered
// agents are skipped. An agent that doesn't support the requested scope
// falls back to user scope with an explanatory note.
func Run(agents []Agent, names []string, binPath string, scope Scope) []Result {
	selected := map[string]bool{}
	for _, n := range names {
		selected[n] = true
	}
	var results []Result
	for _, a := range agents {
		if !selected[a.Name()] {
			continue
		}
		res := Result{Agent: a.Name()}
		st := a.Status(scope)
		switch {
		case !st.Installed:
			res.Err = os.ErrNotExist
		case st.Registered:
			res.OK, res.Skipped = true, true
		default:
			useScope := scope
			if !a.SupportsScope(scope) {
				useScope = ScopeUser
				res.Note = a.Name() + ": " + scope.String() + " scope unsupported, registered at user scope"
			}
			if err := a.Register(binPath, useScope); err != nil {
				res.Err = err
			} else {
				res.OK = true
			}
		}
		results = append(results, res)
	}
	return results
}
```

- [ ] **Step 3: Run tests, verify pass**

Run: `go test -race ./internal/setup/ -v && go vet ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/setup/
git commit -m "feat(setup): detection and registration orchestration"
```

---

### Task 5: The `setup` CLI command

**Files:**
- Create: `cmd/runtimepulse/cmd_setup.go`
- Modify: `cmd/runtimepulse/main.go`

CLI logic stays thin (no unit test; smoke covers it in Task 6). It resolves the binary path, detects, renders a table, prompts or honors flags, runs, reports.

- [ ] **Step 1: Implement**

`cmd/runtimepulse/cmd_setup.go`:

```go
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/tufantunc/RuntimePulse/internal/setup"
)

func setupCmd() *cobra.Command {
	var all, yes, project, dryRun bool
	var only string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Detect installed agent CLIs and register RuntimePulse as their MCP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			binPath, err := os.Executable()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			scope := setup.ScopeUser
			if project {
				scope = setup.ScopeProject
			}

			agents := setup.Registry(home, cwd)
			statuses := setup.DetectAll(agents, scope)

			fmt.Println("Scanning for installed agent CLIs…")
			var installed []string
			for _, s := range statuses {
				mark, label := "✗", "not found"
				if s.Status.Installed {
					mark = "✓"
					label = "not registered"
					if s.Status.Registered {
						label = "already registered"
					}
					if only == "" || only == s.Agent.Name() {
						installed = append(installed, s.Agent.Name())
					}
				}
				fmt.Printf("  %s %-10s %s\n", mark, s.Agent.Name(), label)
			}
			if only != "" {
				installed = filter(installed, only)
			}
			if len(installed) == 0 {
				fmt.Println("\nNo target agent CLIs found.")
				return nil
			}

			if dryRun {
				fmt.Printf("\n[dry-run] would register RuntimePulse (%s scope) for: %s\n",
					scope.String(), strings.Join(installed, ", "))
				return nil
			}

			if !all && !yes && !confirm(installed) {
				fmt.Println("Aborted.")
				return nil
			}

			fmt.Println()
			failed := false
			for _, r := range setup.Run(agents, installed, binPath, scope) {
				switch {
				case r.Err != nil:
					failed = true
					fmt.Printf("  %-10s ✗ %v\n", r.Agent, r.Err)
				case r.Skipped:
					fmt.Printf("  %-10s • already registered\n", r.Agent)
				default:
					line := r.Agent + "  ✓ registered"
					if r.Note != "" {
						line += " (" + r.Note + ")"
					}
					fmt.Printf("  %s\n", line)
				}
			}
			fmt.Println("\nDone. Restart any running agent sessions to pick up the new MCP server.")
			if failed {
				return fmt.Errorf("one or more agents failed to register")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "register every installed agent without prompting")
	cmd.Flags().BoolVar(&yes, "yes", false, "assume yes to the confirmation prompt")
	cmd.Flags().StringVar(&only, "agent", "", "target a single agent (claude|cursor|codex|opencode)")
	cmd.Flags().BoolVar(&project, "project", false, "register in the current project instead of user scope")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would happen, write nothing")
	return cmd
}

func filter(names []string, only string) []string {
	var out []string
	for _, n := range names {
		if n == only {
			out = append(out, n)
		}
	}
	return out
}

// confirm prompts on a TTY; with no TTY it returns false (use --all/--yes).
func confirm(names []string) bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "not a TTY; pass --all or --yes to register non-interactively")
		return false
	}
	fmt.Printf("\nRegister RuntimePulse (MCP) for %s? [Y/n] ", strings.Join(names, ", "))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "" || line == "y" || line == "yes"
}
```

`main.go`: add `setupCmd()` to the `AddCommand(...)` call.

- [ ] **Step 2: Add the term dependency, build, manual smoke**

Run: `go get golang.org/x/term && go mod tidy`
Run: `go build ./... && go vet ./...`
Run (no agents on a clean PATH is fine; just confirm it runs): `go run ./cmd/runtimepulse setup --dry-run`
Expected: prints the scan table and a `[dry-run] would register …` or "No target agent CLIs found."
Windows gate: `GOOS=windows go build ./...`

- [ ] **Step 3: Commit**

```bash
git add cmd/ go.mod go.sum
git commit -m "feat(cli): setup wizard command"
```

---

### Task 6: Smoke + docs

**Files:**
- Modify: `scripts/smoke.sh`, `README.md`, `docs/usage/mcp.md`, `docs/PROJECT.md`

- [ ] **Step 1: Extend the smoke**

The smoke already builds `$rp` and has the mock-claude binary on a custom dir. Add, before the MCP-handshake section, a setup section that registers a mock cursor (file agent — fully hermetic, no real agent needed) by putting a fake `agent` binary on PATH and pointing HOME at the temp dir. Use the `has` helper, never `| grep -q`.

```bash
# --- setup wizard: registers a (file-based) agent into an isolated HOME ---
fake_home="$dir/home"
mkdir -p "$fake_home/bin"
# a stub `agent` binary so cursor is "installed"
printf '#!/bin/sh\nexit 0\n' > "$fake_home/bin/agent"
chmod +x "$fake_home/bin/agent"
# dry-run lists cursor without writing
PATH="$fake_home/bin:$PATH" HOME="$fake_home" "$rp" setup --agent cursor --dry-run > "$dir/setup-dry.txt"
has cursor cat "$dir/setup-dry.txt" || { echo "FAIL: setup --dry-run did not detect cursor"; exit 1; }
[ ! -f "$fake_home/.cursor/mcp.json" ] || { echo "FAIL: dry-run wrote a file"; exit 1; }
# real register
PATH="$fake_home/bin:$PATH" HOME="$fake_home" "$rp" setup --agent cursor --all
has runtimepulse cat "$fake_home/.cursor/mcp.json" || { echo "FAIL: setup did not register cursor"; exit 1; }
# idempotent: second run skips
PATH="$fake_home/bin:$PATH" HOME="$fake_home" "$rp" setup --agent cursor --all > "$dir/setup-2.txt"
has "already registered" cat "$dir/setup-2.txt" || { echo "FAIL: re-run not reported as already registered"; exit 1; }
```

> Note: `setup` resolves HOME via `os.UserHomeDir`, which honors `$HOME` on unix — so the isolated `HOME` keeps the smoke from touching the real `~/.cursor`. The `--agent cursor` flag plus the stub `agent` on PATH exercises detection + file-merge + idempotency end to end without any real agent CLI.

- [ ] **Step 2: Run the smoke**

Run: `./scripts/smoke.sh && ./scripts/smoke.sh`
Expected: `SMOKE OK` twice. (If the stub-on-PATH detection is flaky because the test's own `agent` resolves elsewhere, use an absolute stub dir first on PATH as shown.)

- [ ] **Step 3: Docs**

* `README.md` Install section: after the channels, add — "Then register RuntimePulse with your installed agents: `runtimepulse setup` (detects Claude/Cursor/Codex/OpenCode and wires up the MCP server)."
* `docs/usage/mcp.md`: lead with a "Quick setup" block — `runtimepulse setup` (what it detects, `--all`/`--project`/`--dry-run`, codex-global-only note) — then keep the existing manual per-agent registration as the reference/fallback.
* `docs/PROJECT.md` §7.1 CLI list: add `runtimepulse setup` with a one-line description.

- [ ] **Step 4: Full verification + commit**

Run: `go test -race ./... && go vet ./... && gofmt -l .`
Run: `GOOS=windows go build ./... && GOOS=windows go vet ./...`
Run: `./scripts/smoke.sh`
Expected: all green, `SMOKE OK`

```bash
git add scripts/smoke.sh README.md docs/
git commit -m "test(setup): hermetic setup smoke; document the setup wizard"
```

---

## Self-Review Notes

- **Spec coverage:** Agent interface + two families → Tasks 2–3; jsonmerge safety → Task 1; orchestration (independent failures, skip-already-registered, scope fallback) → Task 4; CLI/flags/TTY → Task 5; scope matrix incl. codex-no-project → `codexAgent.SupportsScope` (Task 3) + `Run` fallback (Task 4); UX table + dry-run → Task 5; testing strategy → per-task tests + smoke (Task 6); docs → Task 6.
- **Type consistency:** `Scope`/`Status`/`Agent`/`Result`/`AgentStatus` defined in setup.go (Tasks 2/4) and used by every agent + the CLI; `runFunc`/`queryFunc` seams in native.go match the `recordCmd.run` test injector; `mergeJSONServer`/`hasJSONServer` signatures (Task 1) match all file-agent call sites.
- **`os.UserHomeDir` honors `$HOME`** on unix → the smoke's isolated HOME is safe and never touches the developer's real `~/.cursor`.
- **Verification honesty:** real `claude mcp add`/`codex mcp add` are NOT exercised (no agent CLIs in CI); native agents are proven via the `runCmd` argv-capture seam, and the smoke proves the file path end to end. The OpenCode global config path is a flagged implementation-time verification.
- **New dependency:** `golang.org/x/term` (TTY detection) — tiny, stdlib-adjacent, already an indirect dep of the toolchain.
```
