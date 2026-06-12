# RuntimePulse Distribution Implementation Plan (Stage 8)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tag-driven releases: `git push origin v1.0.0` produces 6 platform binaries + checksums on GitHub Releases, updates the Homebrew tap, and a checksum-verifying `install.sh` serves curl-pipe installs — with the binary reporting its real version.

**Architecture:** A new `internal/version` package is the single version source (`var Version = "dev"`, ldflags-injected at release, VCS fallback for source builds); daemon/mcpserver/CLI consume it and the CLI warns on daemon/client mismatch. `.goreleaser.yaml` (v2) builds the 6-target matrix with deterministic archive names; `release.yml` runs it on `v*` tags; `ci.yml` gets action bumps + a `goreleaser check` lint. `scripts/install.sh` auto-detects platform and verifies SHA256SUMS.

**Tech Stack:** GoReleaser v2 (~2.16), actions/checkout@v6, actions/setup-go@v6, goreleaser/goreleaser-action@v7, POSIX sh + shellcheck.

**Spec:** `docs/superpowers/specs/2026-06-12-distribution-design.md` (approved; owner decisions: no `go install` channel, tag-push flow, v1.0.0 first release, TAP_GITHUB_TOKEN accepted). The actual v1.0.0 tag push happens AFTER this plan merges and the owner adds the secret — not a plan task.

**File structure:**

```
internal/version/version.go        — Version var + String() with VCS fallback
internal/version/version_test.go
internal/daemon/daemon.go|rpc.go   — drop const Version; use version pkg (modify)
internal/mcpserver/server.go       — drop const Version; use version pkg (modify)
cmd/runtimepulse/main.go           — root.Version + mismatch warning (modify)
cmd/runtimepulse/cmd_daemon.go     — banner uses version pkg (modify)
.goreleaser.yaml                   — builds/archives/checksum/brews
.github/workflows/release.yml      — tag-triggered release
.github/workflows/ci.yml           — v6/v6 bumps + goreleaser check (modify)
scripts/install.sh                 — installer
README.md, docs/RELEASING.md, docs/usage/getting-started.md — docs
```

---

### Task 1: `internal/version` + migration + mismatch warning

**Files:**
- Create: `internal/version/version.go`, `internal/version/version_test.go`
- Modify: `internal/daemon/daemon.go` (remove `const Version`), `internal/daemon/rpc.go` (status), `internal/mcpserver/server.go` (remove `const Version`), `cmd/runtimepulse/main.go`, `cmd/runtimepulse/cmd_daemon.go`

- [ ] **Step 1: Write the failing test**

`internal/version/version_test.go`:

```go
package version

import (
	"strings"
	"testing"
)

func TestStringReturnsInjectedVersion(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "1.0.0"
	if got := String(); got != "1.0.0" {
		t.Fatalf("String() = %q, want injected version", got)
	}
}

func TestStringDevFallback(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "dev"
	got := String()
	// Test binaries have no VCS stamping, so plain "dev" is expected;
	// a source build of the real binary yields "dev (<rev>)".
	if !strings.HasPrefix(got, "dev") {
		t.Fatalf("String() = %q, want dev-prefixed fallback", got)
	}
}

func TestIsDev(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "dev"
	if !IsDev() {
		t.Fatal("dev must report IsDev")
	}
	Version = "1.0.0"
	if IsDev() {
		t.Fatal("release version must not report IsDev")
	}
}
```

Run: `go test ./internal/version/ -v` → FAIL (package missing)

- [ ] **Step 2: Implement**

`internal/version/version.go`:

```go
// Package version is the single source of truth for RuntimePulse's
// version. Release builds inject the git tag at link time:
//
//	-X github.com/tufantunc/RuntimePulse/internal/version.Version=1.0.0
//
// Source builds fall back to VCS metadata from the Go build info, so
// "which commit is this?" is always answerable.
package version

import "runtime/debug"

// Version is "dev" unless overwritten by a release build's ldflags.
var Version = "dev"

// IsDev reports whether this is a non-release build. Used to suppress
// the client/daemon mismatch warning for source builds.
func IsDev() bool { return Version == "dev" }

// String returns the human-facing version: the injected release
// version, or "dev (<short-rev>[+dirty])" for source builds, or plain
// "dev" when no VCS info exists (test binaries).
func String() string {
	if !IsDev() {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Version
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return Version
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		return "dev (" + rev + "+dirty)"
	}
	return "dev (" + rev + ")"
}
```

- [ ] **Step 3: Migrate the consumers**

* `internal/daemon/daemon.go`: delete `const Version = "0.1.0-dev"`.
* `internal/daemon/rpc.go` `status()`: `"version": Version` → `"version": version.String()` (import `github.com/tufantunc/RuntimePulse/internal/version`).
* `cmd/runtimepulse/cmd_daemon.go`: the startup banner's `daemon.Version` → `version.String()`.
* `cmd/runtimepulse/main.go`: `Version: daemon.Version` → `Version: version.String()`.
* `internal/mcpserver/server.go`: delete `const Version`; `mcp.Implementation{... Version: version.String()}`.
* Grep gate: `grep -rn "0.1.0-dev" .` → no hits outside docs/plans.

- [ ] **Step 4: Mismatch warning in the CLI**

In `cmd/runtimepulse/main.go` (imports: `strings`, `os` already present or add):

```go
// dial returns a client, auto-starting the daemon if needed, and warns
// once on a daemon/client version mismatch.
func dial() (*client.Client, error) {
	dir, err := daemon.StateDir()
	if err != nil {
		return nil, err
	}
	c, err := client.EnsureDaemon(dir, daemon.SocketPath(dir))
	if err != nil {
		return nil, err
	}
	warnVersionMismatch(c)
	return c, nil
}

// warnVersionMismatch never fails the command and stays silent for dev
// builds on either side — only released-vs-released mismatches matter.
func warnVersionMismatch(c *client.Client) {
	local := version.String()
	if strings.Contains(local, "dev") {
		return
	}
	var st struct {
		Version string `json:"version"`
	}
	if err := c.Call("status", nil, &st); err != nil {
		return
	}
	if st.Version == "" || strings.Contains(st.Version, "dev") || st.Version == local {
		return
	}
	fmt.Fprintf(os.Stderr,
		"warning: daemon is %s but this client is %s — stop the daemon (it auto-restarts on next use) to update\n",
		st.Version, local)
}
```

- [ ] **Step 5: Verify**

Run: `go test -race ./... && go vet ./... && gofmt -l .` → green/empty
Run: `go build -ldflags "-X github.com/tufantunc/RuntimePulse/internal/version.Version=9.9.9" -o /tmp/rp-vtest ./cmd/runtimepulse && /tmp/rp-vtest --version`
Expected: output contains `9.9.9`
Run: `go run ./cmd/runtimepulse --version`
Expected: output contains `dev (` (VCS-stamped source build)
Windows gate: `GOOS=windows go build ./... && GOOS=windows go vet ./...`
Smoke: `./scripts/smoke.sh` → `SMOKE OK` (status JSON's version value changed shape — the smoke greps don't assert it, but verify).

- [ ] **Step 6: Commit**

```bash
git add internal/ cmd/
git commit -m "feat(version): single ldflags-injectable version source with VCS fallback"
```

---

### Task 2: GoReleaser config + workflows

**Files:**
- Create: `.goreleaser.yaml`, `.github/workflows/release.yml`
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Install goreleaser locally if missing**

Run: `command -v goreleaser || brew install goreleaser`

- [ ] **Step 2: Write `.goreleaser.yaml`**

```yaml
version: 2
project_name: runtimepulse

builds:
  - id: runtimepulse
    main: ./cmd/runtimepulse
    env:
      - CGO_ENABLED=0
    goos: [darwin, linux, windows]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w -X github.com/tufantunc/RuntimePulse/internal/version.Version={{ .Version }}

archives:
  - id: default
    # Deterministic names: install.sh builds latest/download URLs from
    # these without touching the GitHub API.
    name_template: "runtimepulse_{{ .Os }}_{{ .Arch }}"
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    files:
      - LICENSE
      - README.md

checksum:
  name_template: SHA256SUMS

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs"
      - "^test"
      - "^plan"
      - "^build"

brews:
  - name: runtimepulse
    repository:
      owner: tufantunc
      name: homebrew-tap
      token: "{{ .Env.TAP_GITHUB_TOKEN }}"
    homepage: https://github.com/tufantunc/RuntimePulse
    description: "Event-driven session continuation engine for AI coding agents"
    test: |
      system "#{bin}/runtimepulse --version"
```

> **Deprecation check (load-bearing):** GoReleaser has been migrating `brews` toward `homebrew_casks`. Run `goreleaser check` against the pinned local version: if it flags `brews` as deprecated, follow the deprecation notice's replacement (same tap/owner/token fields under the new key) and note the change in the commit message. Also read the repo's `LICENSE` file: if it is a recognizable SPDX license (e.g. MIT), add `license: "<SPDX-ID>"` to the brew section; if not recognizable, omit the field.

- [ ] **Step 3: Validate config**

Run: `goreleaser check`
Expected: `1 configuration file(s) validated` (fix any schema errors/deprecations before continuing)

- [ ] **Step 4: Write `.github/workflows/release.yml`**

```yaml
name: release

on:
  push:
    tags: ["v*"]

permissions:
  contents: write

jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0   # full history for the changelog
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - uses: goreleaser/goreleaser-action@v7
        with:
          distribution: goreleaser
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          TAP_GITHUB_TOKEN: ${{ secrets.TAP_GITHUB_TOKEN }}
```

- [ ] **Step 5: Update `.github/workflows/ci.yml`**

* `actions/checkout@v4` → `@v6` and `actions/setup-go@v5` → `@v6` (clears the Node 20 deprecation warnings).
* Add after the smoke step (Linux leg only — config lint on every push):

```yaml
      - name: goreleaser check
        if: runner.os == 'Linux'
        uses: goreleaser/goreleaser-action@v7
        with:
          distribution: goreleaser
          version: "~> v2"
          args: check
```

- [ ] **Step 6: Snapshot verification (the real gate)**

Run: `goreleaser release --snapshot --clean`
Expected: `dist/` contains 6 archives (`runtimepulse_darwin_arm64.tar.gz`, `..._darwin_amd64`, `..._linux_{amd64,arm64}`, `..._windows_{amd64,arm64}.zip`) + `SHA256SUMS`.
Run the native binary: `./dist/runtimepulse_darwin_arm64*/runtimepulse --version` (path may include a build dir — find it with `find dist -name runtimepulse -type f | grep darwin_arm64`)
Expected: a snapshot version string (e.g. `1.0.0-SNAPSHOT-<sha>` or similar), proving ldflags injection works end to end.
Confirm `dist/` is gitignored: `git status --short | grep dist` → empty (add `dist/` to `.gitignore` if not).

- [ ] **Step 7: Commit**

```bash
git add .goreleaser.yaml .github/ .gitignore
git commit -m "feat(release): goreleaser config, tag-triggered release workflow, ci action bumps"
```

---

### Task 3: `scripts/install.sh`

**Files:**
- Create: `scripts/install.sh`

- [ ] **Step 1: Install shellcheck locally if missing**

Run: `command -v shellcheck || brew install shellcheck`

- [ ] **Step 2: Write the script**

```sh
#!/bin/sh
# RuntimePulse installer: auto-detects OS/arch, downloads the matching
# release archive, verifies it against SHA256SUMS, and installs the
# binary. The user never picks a platform; overrides:
#   VERSION=v1.2.3   install a specific release (default: latest)
#   INSTALL_DIR=...  target directory (default: ~/.local/bin)
set -eu

REPO="tufantunc/RuntimePulse"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${VERSION:-}"

os=$(uname -s)
case "$os" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *)
    echo "error: unsupported OS '$os'." >&2
    echo "Windows: download the zip from https://github.com/$REPO/releases" >&2
    exit 1
    ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *)
    echo "error: unsupported architecture '$arch'" >&2
    exit 1
    ;;
esac

if [ -n "$VERSION" ]; then
  base="https://github.com/$REPO/releases/download/$VERSION"
else
  base="https://github.com/$REPO/releases/latest/download"
fi
archive="runtimepulse_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "downloading $base/$archive"
curl -fsSL -o "$tmp/$archive" "$base/$archive"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"

verify() {
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$tmp" && grep " $archive\$" SHA256SUMS | sha256sum -c - >/dev/null)
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$tmp" && grep " $archive\$" SHA256SUMS | shasum -a 256 -c - >/dev/null)
  else
    echo "error: need sha256sum or shasum to verify the download" >&2
    exit 1
  fi
}
verify
echo "checksum OK"

tar -xzf "$tmp/$archive" -C "$tmp"
mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/runtimepulse" "$INSTALL_DIR/runtimepulse"
echo "installed $("$INSTALL_DIR/runtimepulse" --version) to $INSTALL_DIR/runtimepulse"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: $INSTALL_DIR is not on your PATH — add it to your shell profile" >&2 ;;
esac
```

- [ ] **Step 3: Lint and structure-test**

Run: `chmod +x scripts/install.sh && shellcheck scripts/install.sh`
Expected: no findings.
A full e2e run needs a published release — that happens post-tag (spec §Verification step 5). Until then, dry-test the failure path: `VERSION=v0.0.0-nonexistent sh scripts/install.sh; echo "exit=$?"` → curl fails fast with a clear error, nonzero exit, no partial install.

- [ ] **Step 4: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): checksum-verifying platform-detecting install script"
```

---

### Task 4: Docs

**Files:**
- Modify: `README.md`, `docs/usage/getting-started.md`
- Create: `docs/RELEASING.md`

- [ ] **Step 1: README install section**

Replace the "Install & build" section with:

```markdown
## Install

Runs on macOS, Linux, and Windows 10 1803+.

**Homebrew (macOS/Linux):**

​```bash
brew install tufantunc/tap/runtimepulse
​```

**Install script (macOS/Linux):**

​```bash
curl -fsSL https://raw.githubusercontent.com/tufantunc/RuntimePulse/main/scripts/install.sh | sh
​```

Auto-detects your platform, verifies checksums, installs to `~/.local/bin` (override with `INSTALL_DIR=`, pin with `VERSION=vX.Y.Z`).

**Manual download (all platforms, incl. Windows):** grab the archive for your OS/arch from the [releases page](https://github.com/tufantunc/RuntimePulse/releases) — Windows ships as a zip — and put `runtimepulse` on your PATH.

**From source:** requires Go 1.26+ — `go build -o runtimepulse ./cmd/runtimepulse` (reports its version as `dev (<commit>)`).

Verify a checkout: `go test -race ./... && ./scripts/smoke.sh` (hermetic; must end `SMOKE OK`).
```

(remove the zero-width characters around the inner code fences — they're here only to nest the markdown)

- [ ] **Step 2: getting-started.md**

The Build section becomes "Install" — reference the three channels briefly (link to README) and keep the from-source path for contributors.

- [ ] **Step 3: `docs/RELEASING.md`**

```markdown
# Releasing

Releases are tag-driven: pushing `vX.Y.Z` runs `.github/workflows/release.yml`,
which builds 6 platform archives via GoReleaser, publishes them with
`SHA256SUMS` to GitHub Releases, and pushes the Homebrew formula to
`tufantunc/homebrew-tap`.

## One-time prerequisites

* Repo secret `TAP_GITHUB_TOKEN`: a PAT with write access to
  `tufantunc/homebrew-tap` (the default `GITHUB_TOKEN` cannot write to
  another repository).

## Cutting a release

​```bash
git checkout main && git pull
go test -race ./... && ./scripts/smoke.sh   # everything green?
git tag vX.Y.Z
git push origin vX.Y.Z
gh run watch                                 # the release workflow
​```

Versioning is SemVer; v1.x means breaking CLI/RPC/MCP changes require a
major bump. The tag is the only place a version is written — the binary
gets it via ldflags (`internal/version`).

## Post-release checklist

* Release page shows 6 archives + SHA256SUMS.
* `curl -fsSL .../install.sh | sh` installs and `runtimepulse --version`
  prints the new version.
* `brew update && brew install tufantunc/tap/runtimepulse` (or `brew
  upgrade runtimepulse`) works.
* `runtimepulse status` against a running old daemon prints the version
  mismatch warning (expected until the daemon restarts).
```

(again, strip the zero-width fence escapes)

- [ ] **Step 4: Verify + commit**

Run: link check over changed docs (same python one-liner pattern used for the usage docs), `./scripts/smoke.sh` once more.

```bash
git add README.md docs/
git commit -m "docs: install channels, releasing guide"
```

---

## Post-merge release execution (in-session, NOT subagent tasks)

1. Merge to main; push; CI green ×3 (now with v6 actions + goreleaser check).
2. **Owner adds `TAP_GITHUB_TOKEN`** (PAT with write access to tufantunc/homebrew-tap). Verify with `gh secret list`.
3. `git tag v1.0.0 && git push origin v1.0.0`; `gh run watch` the release workflow.
4. Post-release e2e on this machine: install.sh into a temp INSTALL_DIR → `--version` prints `1.0.0`; `brew install tufantunc/tap/runtimepulse`; release page assets check.

## Self-Review Notes

- **Spec coverage:** §1 version pkg + mismatch warning → Task 1; §3 goreleaser (matrix, deterministic names, SHA256SUMS, brews+deprecation check, license probe) → Task 2; §4 release.yml + owner manual step → Task 2 + post-merge §2; §5 CI bumps + goreleaser check → Task 2; §6 install.sh → Task 3; §7 docs → Task 4; verification §1–5 → per-task gates + post-merge section.
- **Type consistency:** `version.Version`/`String()`/`IsDev()` defined in Task 1, consumed in Tasks 1 (CLI/daemon/mcpserver) and 2 (ldflags path string matches the package path).
- **Honesty:** install.sh full e2e and brew install can only run after the first real release — the plan says so explicitly and tests the failure path beforehand.
- **ldflags uses `{{ .Version }}`** (no leading `v`) so `--version` prints `1.0.0` — consistent with the spec's post-release expectation.
```
