# Distribution — Design

**Date:** 2026-06-12
**Status:** Approved
**Precondition met:** Windows support merged; 3-OS CI green on github.com/tufantunc/RuntimePulse (run 27405188177).

## Goal

Users install RuntimePulse without Go and without picking a platform by hand: Homebrew (`brew install tufantunc/tap/runtimepulse`), a checksum-verifying install script (`curl | sh`), or manual download from GitHub Releases. Releases are cut by pushing a git tag; the binary knows its version. `go install` was explicitly eliminated by the owner (requires Go on the user's machine).

## Decisions

### 1. Version management: `internal/version`, git tags as the source of truth

* New package `internal/version`: `var Version = "dev"` (a var, so ldflags can write it) and `String()` — returns `Version` when set by a release build; otherwise falls back to `debug.ReadBuildInfo` VCS data (`dev (a1b2c3d)`, `+dirty` when modified) so source builds still answer "which commit is this?".
* The duplicated `const Version = "0.1.0-dev"` in `internal/daemon` and `internal/mcpserver` is removed; both (and the CLI root) consume `internal/version`.
* Release builds inject the tag via `-X github.com/tufantunc/RuntimePulse/internal/version.Version={{.Version}}`. No hand-edited version strings anywhere, ever.
* **Client/daemon mismatch warning:** the CLI compares its own version with the daemon's `status` version on connect and prints one stderr warning when they differ ("daemon vX, client vY — restart the daemon to update"). Suppressed when either side reports a dev version. The check must never fail the command.

### 2. First release: v1.0.0 (owner decision)

The spec roadmap is complete, so the owner chose to start at v1.0.0 — accepting the SemVer contract that breaking CLI/RPC/MCP surface changes from here on require a major bump. Tags are `vMAJOR.MINOR.PATCH`.

### 3. `.goreleaser.yaml` (GoReleaser v2)

* Single build: `cmd/runtimepulse`, `CGO_ENABLED=0`, `goos: [darwin, linux, windows]` × `goarch: [amd64, arm64]` (6 artifacts), ldflags `-s -w -X .../internal/version.Version={{.Version}}`.
* Archives: tar.gz, `format_overrides` zip for Windows; include LICENSE and README. **Deterministic name template** (`runtimepulse_{{ .Os }}_{{ .Arch }}`) so the install script can build `releases/latest/download/...` URLs without calling the GitHub API.
* `SHA256SUMS` checksum file. No artifact signing in v1 (cosign noted as future work).
* Homebrew formula published to `tufantunc/homebrew-tap` (exists, public) using the `TAP_GITHUB_TOKEN` secret. GoReleaser v2.16's current non-deprecated brew mechanism is verified at implementation time with `goreleaser check` (the `brews`→casks migration in recent GoReleaser versions must be checked against the pinned version, not assumed).

### 4. Release flow: tag-push driven (owner decision)

`.github/workflows/release.yml`: triggers on `v*` tag push; `permissions: contents: write`; checkout@v6 with `fetch-depth: 0` (changelog), setup-go@v6, goreleaser-action@v7 running `release --clean`; env `GITHUB_TOKEN` (releases) + `TAP_GITHUB_TOKEN` (tap push). Local `goreleaser release` is not used (tokens stay in repo secrets; runs are reproducible).

**Owner's one-time manual step:** create a PAT with `repo` scope for the tap and add it to RuntimePulse as the `TAP_GITHUB_TOKEN` secret. The default `GITHUB_TOKEN` cannot write to another repository.

### 5. CI updates (`ci.yml`)

* Action bumps: checkout v4→v6, setup-go v5→v6 (verified latest majors; clears the Node 20 deprecation warnings).
* The ubuntu leg gains a fast `goreleaser check` step so a broken release config is caught on every push, not on tag day.

### 6. `scripts/install.sh`

POSIX sh. Detects OS/arch via `uname` (`x86_64`→amd64, `aarch64`/`arm64`→arm64); on unsupported platforms (incl. Windows shells) prints a clear pointer to the Releases page. Downloads `releases/latest/download/runtimepulse_<os>_<arch>.tar.gz` (or `VERSION=vX.Y.Z` override → `releases/download/vX.Y.Z/...`), verifies against `SHA256SUMS` (`sha256sum` or `shasum -a 256`), installs to `${INSTALL_DIR:-$HOME/.local/bin}`, warns when the dir is not on PATH. Auto-detection is mandatory — the user chooses channel/version/dir, never platform. Passes shellcheck; respects the repo's pipefail/SIGPIPE lessons.

### 7. Docs

* README install section rewritten around the three channels (brew, `curl | sh`, manual download with the Windows zip note) and the supported-platforms line.
* New `docs/RELEASING.md`: prerequisites (TAP_GITHUB_TOKEN), the flow (tag → push → watch the release workflow), verification checklist (release assets present, checksums match, brew formula updated, install.sh works against the new tag).
* `docs/usage/getting-started.md` build section gains the install channels.

## Verification & first release

1. Local gates: `goreleaser check`, `goreleaser release --snapshot --clean` (6 binaries in `dist/`; run one and assert `--version` shows the snapshot version), `shellcheck scripts/install.sh`, full `go test -race ./...` + smoke.
2. CI green on the bumped actions.
3. Owner adds `TAP_GITHUB_TOKEN`.
4. `git tag v1.0.0 && git push origin v1.0.0`; watch the release workflow.
5. Post-release e2e: `install.sh` against the published release on this machine, `brew install tufantunc/tap/runtimepulse`, `runtimepulse --version` reports `1.0.0`.

## Out of scope (deliberate)

Artifact signing (cosign), Scoop/winget manifests, Linux distro packages (deb/rpm/nfpm), automated changelog beyond GoReleaser's default commit list, release announcement automation.
