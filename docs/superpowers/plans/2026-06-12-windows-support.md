# RuntimePulse Windows Support Implementation Plan (Stage 7)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** RuntimePulse builds, tests green, and operates on Windows 10 1803+ (amd64/arm64), proven by a 3-OS GitHub Actions matrix — the gate for the upcoming distribution work.

**Spec:** `docs/superpowers/specs/2026-06-12-windows-support-design.md` (approved; owner decisions: AF_UNIX everywhere, gopsutil on ALL platforms, CI in scope). Do not re-litigate decisions recorded there.

**Architecture:** Three build-tagged pairs (single-instance lock, socket chmod, autostart detach); `pgrep` replaced by gopsutil/v4 with case-sensitive-substring-of-cmdline semantics everywhere; sh mock scripts replaced by the re-exec helper pattern (`internal/mockexe` + per-package TestMain); a windows test-compat sweep (`/tmp` hardcodes, perm-check skips); CI matrix.

**Local verification limits:** there is no Windows machine here. Every task gates on `GOOS=windows go build ./...` + `GOOS=windows go vet ./...` (cross-compile catches API misuse) plus the full unix suite; the real Windows test run happens in CI after push. `go test` does NOT cross-execute — do not claim Windows tests pass locally.

**File structure:**

```
internal/daemon/lock_unix.go|lock_windows.go   — acquireLock split (moved out of daemon.go)
internal/daemon/sock_unix.go|sock_windows.go   — chmodSocket helper
internal/client/detach_unix.go|detach_windows.go — detachSysProcAttr
internal/mockexe/mockexe.go                    — re-exec mock helper (lib, used by tests)
internal/watch/probers.go                      — gopsutil ProcessProber (modify)
internal/{adapter,daemon,watch,mcpserver}/..._test.go — TestMain + mock migration, /tmp sweep
.github/workflows/ci.yml                       — 3-OS matrix
README.md, docs/usage/{watchers,getting-started}.md — platform docs (modify)
```

---

### Task 1: Build-tagged platform shims (lock, socket chmod, detach)

**Files:**
- Create: `internal/daemon/lock_unix.go`, `internal/daemon/lock_windows.go`, `internal/daemon/sock_unix.go`, `internal/daemon/sock_windows.go`, `internal/client/detach_unix.go`, `internal/client/detach_windows.go`
- Modify: `internal/daemon/daemon.go` (remove acquireLock + unix import; use chmodSocket), `internal/client/ensure.go` (use detachSysProcAttr), `internal/daemon/daemon_test.go` (lock test + perm-test skip)

- [ ] **Step 1: Write the failing test (append to daemon_test.go)**

```go
func TestAcquireLockExclusive(t *testing.T) {
	dir := shortTempDir(t)
	f, err := acquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if second, err := acquireLock(dir); err == nil {
		second.Close()
		t.Fatal("second acquireLock on the same dir must fail while the first is held")
	}
}
```

(Currently passes against the existing unix implementation — that's fine; it pins behavior across the split. Run it first to confirm green, then refactor.)

- [ ] **Step 2: Split the lock**

`internal/daemon/lock_unix.go`:

```go
//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// acquireLock takes an exclusive, non-blocking flock on dir/daemon.lock
// for the daemon's lifetime. It serializes socket acquisition across
// concurrent autostarts: net.Listen("unix") is bind-then-listen, so an
// unguarded stale-socket probe could unlink a live daemon's socket in
// the window between the two. Released automatically on process death.
func acquireLock(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("daemon already starting or running (lock held): %w", err)
	}
	return f, nil
}
```

`internal/daemon/lock_windows.go`:

```go
//go:build windows

package daemon

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// acquireLock is the Windows counterpart of the unix flock: an
// exclusive, fail-immediately LockFileEx on dir/daemon.lock, held for
// the daemon's lifetime and released automatically on process death.
func acquireLock(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, ol); err != nil {
		f.Close()
		return nil, fmt.Errorf("daemon already starting or running (lock held): %w", err)
	}
	return f, nil
}
```

Delete `acquireLock` (and its doc comment) from `daemon.go`; drop the `golang.org/x/sys/unix` import there.

- [ ] **Step 3: Socket chmod helper**

`internal/daemon/sock_unix.go`:

```go
//go:build !windows

package daemon

import "os"

// chmodSocket restricts the daemon socket to the owning user (spec §9).
func chmodSocket(path string) error { return os.Chmod(path, 0o600) }
```

`internal/daemon/sock_windows.go`:

```go
//go:build windows

package daemon

// chmodSocket is a no-op on Windows: there is no 0600 equivalent for an
// AF_UNIX socket file. Access control relies on the user-private ACLs
// of %USERPROFILE%\.runtimepulse (windows-support design spec, §1).
func chmodSocket(string) error { return nil }
```

In `daemon.go`'s `New`, replace `os.Chmod(sock, 0o600)` with `chmodSocket(sock)`.

- [ ] **Step 4: Detach helper**

`internal/client/detach_unix.go`:

```go
//go:build !windows

package client

import "syscall"

// detachSysProcAttr makes the autostarted daemon survive parent exit.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
```

`internal/client/detach_windows.go`:

```go
//go:build windows

package client

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// detachSysProcAttr makes the autostarted daemon survive parent exit.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
}
```

In `ensure.go`, replace the `cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}` line with `cmd.SysProcAttr = detachSysProcAttr()` (keep the comment); the `syscall` import in ensure.go goes away.

- [ ] **Step 5: Skip the perm test on Windows**

`TestSocketPermissions` in daemon_test.go gains as its first line (import `runtime`):

```go
	if runtime.GOOS == "windows" {
		t.Skip("socket file permissions are not meaningful on Windows (ACL model)")
	}
```

- [ ] **Step 6: Verify**

Run: `go test -race ./internal/daemon/ ./internal/client/ && go vet ./... && gofmt -l .`
Then the Windows gate: `GOOS=windows go build ./... && GOOS=windows go vet ./...`
Expected: all clean. (`go get golang.org/x/sys@latest` if the windows subpackage is missing from go.sum — likely already present via the unix subpackage.)

- [ ] **Step 7: Commit**

```bash
git add internal/daemon/ internal/client/
git commit -m "feat(windows): build-tagged lock, socket chmod and autostart detach"
```

---

### Task 2: `internal/mockexe` + migrate all sh test mocks

**Files:**
- Create: `internal/mockexe/mockexe.go`, `internal/mockexe/mockexe_test.go`
- Modify: `internal/adapter/run_test.go` (TestMain + mockBin rewrite), `internal/adapter/{claude,cursor,codex,opencode}_test.go` (call sites), `internal/daemon/dispatch_rpc_test.go` + a TestMain for the daemon package, `internal/mcpserver` if it spawns mocks (audit: it uses the daemon's RUNTIMEPULSE_CLAUDE_BIN only via dispatch tests — check `grep -rn "mock" internal/mcpserver/`)

The re-exec pattern: the test binary doubles as the mock CLI. `TestMain` calls `mockexe.Main()` first; when the sentinel env var is set (only true in spawned children), it performs the mocked behavior and exits instead of running tests.

- [ ] **Step 1: Write mockexe + its own test**

`internal/mockexe/mockexe.go`:

```go
// Package mockexe lets a test binary impersonate an agent CLI: call
// Main() first in TestMain; in child processes spawned with Env(spec)
// it performs the configured behavior and exits. This replaces
// #!/bin/sh mock scripts so adapter/daemon tests run on Windows.
package mockexe

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const sentinel = "RUNTIMEPULSE_TEST_MOCK"

// Spec describes the mocked CLI's behavior. In Stdout/Stderr the token
// {LAST} is replaced with the process's last argv element — that is how
// dash-prompt tests prove the prompt arrived intact.
type Spec struct {
	Stdout  string
	Stderr  string
	Exit    int
	DelayMs int
}

// Env returns the environment entries that make a child running the
// test binary behave per spec. Append to os.Environ() — or set them via
// t.Setenv so children inherit them implicitly.
func Env(s Spec) []string {
	return []string{
		sentinel + "=1",
		"MOCK_STDOUT=" + s.Stdout,
		"MOCK_STDERR=" + s.Stderr,
		"MOCK_EXIT=" + strconv.Itoa(s.Exit),
		"MOCK_DELAY_MS=" + strconv.Itoa(s.DelayMs),
	}
}

// Main must be the first call in TestMain. No-op in the test process;
// in a child spawned with Env(spec) it acts as the mock and never
// returns.
func Main() {
	if os.Getenv(sentinel) != "1" {
		return
	}
	if d, _ := strconv.Atoi(os.Getenv("MOCK_DELAY_MS")); d > 0 {
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
	last := ""
	if len(os.Args) > 1 {
		last = os.Args[len(os.Args)-1]
	}
	expand := func(s string) string { return strings.ReplaceAll(s, "{LAST}", last) }
	if out := os.Getenv("MOCK_STDOUT"); out != "" {
		fmt.Print(expand(out))
	}
	if errOut := os.Getenv("MOCK_STDERR"); errOut != "" {
		fmt.Fprint(os.Stderr, expand(errOut))
	}
	code, _ := strconv.Atoi(os.Getenv("MOCK_EXIT"))
	os.Exit(code)
}
```

`internal/mockexe/mockexe_test.go` — prove the round trip through a real child process:

```go
package mockexe

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	Main()
	os.Exit(m.Run())
}

func TestChildBehavesPerSpec(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "ignored", "- dash last arg")
	cmd.Env = append(os.Environ(), Env(Spec{Stdout: `{"result":"{LAST}"}`, Exit: 0})...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "- dash last arg") {
		t.Fatalf("LAST expansion failed: %s", out)
	}

	cmd = exec.Command(exe)
	cmd.Env = append(os.Environ(), Env(Spec{Stderr: "boom", Exit: 3})...)
	out2, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if err == nil || !errAs(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("exit code not honored: %v", err)
	}
	if !strings.Contains(string(out2), "boom") {
		t.Fatalf("stderr missing: %s", out2)
	}
}

func errAs(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}
```

Run: `go test ./internal/mockexe/ -v` → must pass (FAIL first only if Main is stubbed — fine to implement directly here, the test IS the contract).

- [ ] **Step 2: Migrate the adapter package**

In `internal/adapter/run_test.go`:
- Add a `TestMain`:

```go
func TestMain(m *testing.M) {
	mockexe.Main()
	os.Exit(m.Run())
}
```

- Replace `mockBin(t, script)` with:

```go
// mockBin configures this test binary as the mocked CLI (re-exec
// pattern; see internal/mockexe) and returns its path. Spec env vars
// are set with t.Setenv so spawned children inherit them.
func mockBin(t *testing.T, spec mockexe.Spec) string {
	t.Helper()
	for _, kv := range mockexe.Env(spec) {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}
```

- Convert every call site in run_test.go and the four adapter test files. The sh scripts map mechanically:

| Old script | New spec |
|---|---|
| `echo '{"result":"ok from mock"}'` | `mockexe.Spec{Stdout: "{\"result\":\"ok from mock\"}"}` |
| echo-last-arg variants (`for last; do :; done; printf ... "$last"`) | `mockexe.Spec{Stdout: "{\"result\":\"{LAST}\"}"}` (or the codex/opencode plain-text shapes with `{LAST}`) |
| `echo 'denied' >&2; exit 1` | `mockexe.Spec{Stderr: "denied", Exit: 1}` |
| `sleep 5` | `mockexe.Spec{DelayMs: 5000}` |
| `echo 'thinking...' >&2; ...stdout...` | `mockexe.Spec{Stderr: "thinking...", Stdout: "did: {LAST}"}` |

Caution: tests that previously asserted an exact summary now receive `{LAST}` already expanded by the child — assertions stay byte-identical. The Claude args end with the prompt (it IS the last arg thanks to the `--` terminator), same for cursor/codex/opencode — which is exactly what the dash tests pin.

- One semantic check to preserve: `TestRunCLISpawnFailureIsError` (missing binary) is unaffected. `TestCursorBinFallsBackWithoutEnv` is unaffected.

- [ ] **Step 3: Migrate the daemon package**

`internal/daemon` needs its own `TestMain` (any _test.go in the package, e.g. daemon_test.go) calling `mockexe.Main()`. Replace `mockClaude(t, script)` in dispatch_rpc_test.go with the spec-based version (sets `RUNTIMEPULSE_CLAUDE_BIN` to the test executable via `t.Setenv`, plus the mockexe env):

```go
func mockClaude(t *testing.T, spec mockexe.Spec) {
	t.Helper()
	for _, kv := range mockexe.Env(spec) {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIMEPULSE_CLAUDE_BIN", exe)
}
```

Call sites: `mockClaude(t, mockexe.Spec{Stdout: "{\"result\":\"manual run ok\"}"})` etc. Same for `TestCursorSessionDispatchesViaMock` (RUNTIMEPULSE_CURSOR_BIN). Audit `internal/mcpserver` for sh mocks (`grep -rn "#!/bin/sh" internal/`) and migrate any stragglers identically (mcpserver tests would also need a TestMain if they spawn mocks).

- [ ] **Step 4: Verify no sh remains in Go tests**

Run: `grep -rn "#!/bin/sh" internal/ && echo "FAIL: sh mocks remain" || echo OK` → OK
Run: `go test -race ./... && go vet ./... && gofmt -l .` → all green
Windows gate: `GOOS=windows go vet ./...`

- [ ] **Step 5: Commit**

```bash
git add internal/
git commit -m "test: replace sh mock scripts with re-exec mockexe helper"
```

---

### Task 3: gopsutil ProcessProber (all platforms)

**Files:**
- Modify: `internal/watch/probers.go`, `internal/watch/probers_test.go` (marker process via mockexe), `go.mod`
- Modify: `internal/mcpserver/tools.go` (jsonschema wording: "pgrep pattern" → "command-line substring")

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/shirou/gopsutil/v4 && go mod tidy`

- [ ] **Step 2: Rewrite the prober test (cross-platform marker process)**

`internal/watch/probers_test.go` — the package needs a `TestMain` calling `mockexe.Main()`; replace `TestProcessProber`:

```go
func TestProcessProber(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := fmt.Sprintf("rp-marker-%d", os.Getpid())
	cmd := exec.Command(exe, marker) // marker lands in the child's cmdline
	cmd.Env = append(os.Environ(), mockexe.Env(mockexe.Spec{DelayMs: 30000})...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()

	p := ProcessProber(marker)
	ctx := context.Background()
	// process tables refresh asynchronously on some platforms; poll briefly
	deadline := time.Now().Add(3 * time.Second)
	for !p(ctx) {
		if time.Now().After(deadline) {
			t.Fatal("running marker process must be found")
		}
		time.Sleep(50 * time.Millisecond)
	}

	cmd.Process.Kill()
	cmd.Wait()
	deadline = time.Now().Add(3 * time.Second)
	for p(ctx) {
		if time.Now().After(deadline) {
			t.Fatal("dead marker process must not be found")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestProcessProberExcludesSelf(t *testing.T) {
	// our own test-binary path is in our own cmdline; the prober must
	// not report US as a match for it
	self, _ := os.Executable()
	p := ProcessProber(self + " unique-never-spawned-suffix")
	if p(context.Background()) {
		t.Fatal("prober must not match a pattern only present in nonexistent processes")
	}
}
```

(remove the old pgrep-based test and its `exec.LookPath("pgrep")` skip; drop unused imports; add `mockexe`, `time` as needed)

Run: `go test ./internal/watch/ -run TestProcessProber -v` → FAIL (prober still pgrep-based; on unix the old code may even pass the first loop — the real gate is after Step 3)

- [ ] **Step 3: Rewrite the prober**

In `internal/watch/probers.go`, replace `ProcessProber` (drop the `os/exec` import if now unused; add `os`, `strings`, `github.com/shirou/gopsutil/v4/process`):

```go
// ProcessProber: running = some process's full command line contains
// pattern as a case-sensitive substring. Implemented with gopsutil on
// every platform (owner decision, windows-support spec §2) — identical
// semantics on macOS/Linux/Windows, no pgrep dependency. The prober's
// own process (the daemon) is excluded; as with pgrep before it, a
// pattern matching another runtimepulse invocation's argv will match.
func ProcessProber(pattern string) Prober {
	self := int32(os.Getpid())
	return func(ctx context.Context) bool {
		procs, err := process.ProcessesWithContext(ctx)
		if err != nil {
			return false
		}
		for _, p := range procs {
			if p.Pid == self {
				continue
			}
			cmdline, err := p.CmdlineWithContext(ctx)
			if err != nil || cmdline == "" {
				continue // permission-denied or exited processes are not matches
			}
			if strings.Contains(cmdline, pattern) {
				return true
			}
		}
		return false
	}
}
```

- [ ] **Step 4: Wording sweep**

`internal/mcpserver/tools.go` CreateWatchInput target description: `pgrep pattern` → `command-line substring`. `cmd/runtimepulse/cmd_watch.go` process flag usage: `pgrep -f pattern` → `command-line substring to match`.

- [ ] **Step 5: Verify**

Run: `go test -race -count=2 ./internal/watch/ && go test ./... && go vet ./... && gofmt -l .`
Windows gate: `GOOS=windows go build ./... && GOOS=windows go vet ./...`
Also: `grep -rn "pgrep" internal/ cmd/ | grep -v _test` → only historical comments allowed; fix any functional remnant.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/ cmd/
git commit -m "feat(watch): gopsutil process prober with identical cross-platform semantics"
```

---

### Task 4: Windows test-compat sweep + CI + docs

**Files:**
- Modify: every `_test.go` hardcoding `/tmp` (audit: `grep -rn 'MkdirTemp("/tmp"' internal/`), `.github/workflows/ci.yml` (create), `README.md`, `docs/usage/watchers.md`, `docs/usage/getting-started.md`

- [ ] **Step 1: /tmp sweep**

`shortTempDir` (daemon_test.go) and `newTestDaemonClient`'s MkdirTemp (mcpserver tests) — and any other `/tmp` hardcode — become platform-aware:

```go
// shortTempDir returns a temp dir whose path is short enough for unix
// socket limits (~104 bytes on macOS). On Windows the default temp dir
// is fine; on unix t.TempDir can exceed the limit, so use /tmp.
func shortTempDir(t *testing.T) string {
	t.Helper()
	root := "/tmp"
	if runtime.GOOS == "windows" {
		root = "" // os default (%TEMP%)
	}
	dir, err := os.MkdirTemp(root, "rp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
```

Apply the same pattern wherever `/tmp` is hardcoded in tests (NOT in smoke.sh — it stays unix-only). Also audit for tests using `/tmp/x`-style repo paths as session repoPaths (e.g. `RepoPath: "/tmp"`) — these don't need to exist for mock dispatch... EXCEPT they do: `runCLI` sets `cmd.Dir`, and exec fails if the dir doesn't exist. On Windows `/tmp` does not exist → those tests would break. Replace literal `"/tmp"` repoPaths in tests with `t.TempDir()`.

- [ ] **Step 2: CI workflow**

`.github/workflows/ci.yml`:

```yaml
name: ci
on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go build ./...
      - run: go vet ./...
      - run: go test -race ./...
      - name: gofmt
        if: runner.os != 'Windows'
        run: test -z "$(gofmt -l .)"
      - name: smoke
        if: runner.os != 'Windows'
        run: ./scripts/smoke.sh
```

- [ ] **Step 3: Docs**

* `README.md`: Install section gains a platform line — "Runs on macOS, Linux, and Windows 10 1803+ (AF_UNIX requirement)." Supported-agents note unchanged.
* `docs/usage/watchers.md` process section: pattern semantics now "a case-sensitive substring of the process's full command line (identical on every platform)"; drop the pgrep references.
* `docs/usage/getting-started.md`: state-dir section notes Windows path (`%USERPROFILE%\.runtimepulse`) and the ACL-based security model (no 0600 on Windows; the directory inherits user-private profile ACLs).
* `docs/PROJECT.md` §9: add one sentence on the Windows security model; §10 stack table: process watching row → gopsutil.

- [ ] **Step 4: Full verification**

```bash
go test -race ./... && go vet ./... && gofmt -l .
GOOS=windows go build ./... && GOOS=windows go vet ./...
GOOS=windows GOARCH=arm64 go build ./...
./scripts/smoke.sh && ./scripts/smoke.sh
grep -rn '"/tmp' internal/ | grep _test   # only platform-guarded uses may remain
```

All green; smoke ×2 `SMOKE OK`.

- [ ] **Step 5: Commit**

```bash
git add .github/ internal/ docs/ README.md
git commit -m "feat(windows): test-compat sweep, 3-OS CI matrix and platform docs"
```

---

## Self-Review Notes

- **Spec coverage:** design §1 (AF_UNIX + chmod no-op) → Task 1; §2 (gopsutil everywhere + wording) → Task 3; §3/§4 (lock/detach pairs) → Task 1; §5 (re-exec mocks) → Task 2; §7 (CI) → Task 4; §8 docs → Task 4. Out-of-scope items untouched.
- **Order rationale:** mockexe (Task 2) must precede the prober rewrite (Task 3) because the new prober test uses mockexe for its marker process.
- **Type consistency:** `mockexe.Spec/Env/Main` used identically in adapter/daemon/watch test packages; `acquireLock`/`chmodSocket`/`detachSysProcAttr` keep existing call-site signatures.
- **Honesty about verification:** local gates are cross-compile (`GOOS=windows go build/vet`) + unix suite; actual Windows test execution happens only in CI after push — the final report must say so, not claim Windows-green locally.
- **Known risk:** AF_UNIX-on-Windows is exercised first in CI; if `net.Listen("unix")` misbehaves there, the documented fallback is named pipes (spec risk table) — stop and escalate rather than improvising.
