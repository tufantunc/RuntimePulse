# Windows Support — Design

**Date:** 2026-06-12
**Status:** Approved
**Why now:** distribution (GoReleaser releases, install script, Homebrew tap) is the next milestone; the owner requires Windows in the release matrix first.

## Goal & target

RuntimePulse builds, passes its test suite, and operates (daemon, watchers, dispatcher, MCP server, CLI) on **Windows 10 1803+ (amd64, arm64)**, alongside the existing macOS/Linux support. CI proves it on every commit.

The 1803 floor comes from AF_UNIX support (decision 1). The CGO-free stack (modernc sqlite, fsnotify, coder/websocket) already compiles for Windows.

## Decisions

### 1. IPC: AF_UNIX everywhere (owner decision)

Go's `net` package supports unix sockets on Windows 10 1803+. All five `"unix"` call sites (daemon listen, stale-socket probe, client dial ×2, follow) stay **unchanged** — one code path, no new dependency. Rejected: named pipes via `go-winio` (Windows-idiomatic, SDDL ACLs, but a second IPC code path in daemon+client plus a dependency).

Consequence: the socket `os.Chmod(0600)` moves behind a build-tagged helper — chmod on unix, no-op on Windows. **Security model note (documented):** on Windows, access control relies on `%USERPROFILE%\.runtimepulse` inheriting the user-private default ACLs of the profile directory; there is no 0600-equivalent on the socket itself.

### 2. Process watcher: gopsutil/v4 on ALL platforms (owner decision)

`ProcessProber` drops the `pgrep -f` subprocess entirely. `github.com/shirou/gopsutil/v4/process` enumerates processes and the prober matches the pattern against each full command line. Owner explicitly chose the library over `tasklist` (name-only matching) and PowerShell CIM (slow), and chose **all platforms** over Windows-only to keep one code path and identical semantics everywhere.

Semantics change (documented in usage docs): the pattern is a **case-sensitive substring of the full command line** — a deliberate simplification from pgrep's regex, predictable and identical on every OS. The prober excludes its own PID (the daemon process, since the prober runs in-process); as today, a pattern that happens to match another `runtimepulse` invocation's argv will match it — same caveat pgrep had, kept documented.

This deviates from the "CLI subprocesses, no SDKs" convention recorded for docker — by owner decision, scoped to process probing. The docker watcher stays subprocess-based.

### 3. Single-instance lock: build-tagged pair

`acquireLock(dir)` keeps its signature; the body splits into `lock_unix.go` (existing `unix.Flock`) and `lock_windows.go` (`windows.LockFileEx`, exclusive + fail-immediately, via `golang.org/x/sys/windows`). Both release on process death.

### 4. Daemon autostart detach: build-tagged pair

`client/ensure.go`'s `Setsid` moves behind `detachSysProcAttr() *syscall.SysProcAttr`: unix returns `{Setsid: true}`; Windows returns `{CreationFlags: DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP}`.

### 5. Test mocks: re-exec helper pattern (removes the sh dependency)

Six test files write `#!/bin/sh` mock scripts. They migrate to Go's standard re-exec pattern: `TestMain` checks a sentinel env var (`RUNTIMEPULSE_TEST_MOCK=1`) and, when set, behaves as the mocked agent CLI — emitting stdout/stderr, exit code, optional delay, optional echo-last-arg, all driven by env vars — then exits. `mockBin(t, script)` call sites become `mockBin(t, mockSpec{Stdout: ..., Exit: ..., Delay: ..., EchoLastArg: ...})` returning the test executable's own path. Works identically on Windows; no shell anywhere in Go tests.

`scripts/smoke.sh` stays bash and unix-only. Windows end-to-end coverage comes from the Go test suite (the daemon RPC tests already exercise watch → event → rule → mock-dispatch → chaining paths).

### 6. Adapters on Windows: no code change, one documented limitation

`exec.LookPath` resolves `.exe` via PATHEXT automatically. npm-installed CLIs that ship as `.cmd` shims carry cmd.exe quoting risks under Go's exec; the documented recommendation is native `.exe` installs or pointing `RUNTIMEPULSE_<AGENT>_BIN` at the real executable.

### 7. CI: GitHub Actions, 3-OS matrix (owner decision: in scope)

`.github/workflows/ci.yml`: `{ubuntu-latest, macos-latest, windows-latest}` × (`go build ./...`, `go vet ./...`, `go test -race ./...`). `gofmt -l` check and `./scripts/smoke.sh` run on the unix legs only. During development, fast local feedback comes from `GOOS=windows go build ./...` + `GOOS=windows go vet ./...`; the real Windows test run happens in CI.

### 8. Unchanged on purpose

* State dir stays `~/.runtimepulse` (`os.UserHomeDir` → `%USERPROFILE%\.runtimepulse`); `RUNTIMEPULSE_DIR` override unchanged.
* Signal handling: `signal.NotifyContext(..., SIGINT, SIGTERM)` compiles on Windows; Ctrl-C maps to Interrupt. No change.
* fsnotify file/git watchers: the library supports Windows (ReadDirectoryChangesW); timing-sensitive tests may need wider waits on Windows CI — widen waits, never weaken assertions.
* WebSocket server (127.0.0.1 + token) — platform-independent.

## Out of scope (deliberate)

* Windows service integration (sc.exe/NSSM/winsw) — the auto-start-on-first-command model works as-is; service wrappers are a user-side concern for now.
* PowerShell port of smoke.sh.
* Special-casing `.cmd` shim execution.
* The distribution work itself (GoReleaser/install script/tap) — next project, gated on this one.

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| AF_UNIX-on-Windows edge cases (Go stdlib paths less traveled) | CI runs the full daemon/client suite on windows-latest from day one; named pipes remain the documented fallback plan if something fundamental surfaces |
| gopsutil behavior drift vs pgrep on unix | Prober unit tests assert substring semantics with a self-spawned marker process, same as today |
| fsnotify timing differences on Windows CI | Wider waits allowed; assertions untouched |
| `-race` slowness on windows-latest runners | Acceptable; matrix legs run in parallel |

## Acceptance

* `go test -race ./...` green on ubuntu/macos/windows in CI; smoke green on unix legs.
* `GOOS=windows go build ./...` and `go vet` clean.
* No `#!/bin/sh` left in any Go test; no `pgrep`/`unix.Flock`/raw `Setsid` outside build-tagged files.
* Docs updated: README platform line, watchers.md process semantics, getting-started.md Windows paths + ACL note.
