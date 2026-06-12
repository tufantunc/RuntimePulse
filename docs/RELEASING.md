# Releasing

Releases are tag-driven: pushing `vX.Y.Z` runs `.github/workflows/release.yml`,
which builds 6 platform archives via GoReleaser, publishes them with
`SHA256SUMS` to GitHub Releases, and pushes the Homebrew cask to
`tufantunc/homebrew-tap`.

## One-time prerequisites

* Repo secret `TAP_GITHUB_TOKEN`: a PAT with write access to
  `tufantunc/homebrew-tap` (the default `GITHUB_TOKEN` cannot write to
  another repository).

## Cutting a release

```bash
git checkout main && git pull
go test -race ./... && ./scripts/smoke.sh   # everything green?
git tag vX.Y.Z
git push origin vX.Y.Z
gh run watch                                 # the release workflow
```

Versioning is SemVer; v1.x means breaking CLI/RPC/MCP changes require a
major bump. The tag is the only place a version is written — the binary
gets it via ldflags (`internal/version`).

## Post-release checklist

* Release page shows 6 archives + SHA256SUMS.
* `curl -fsSL .../install.sh | sh` installs and `runtimepulse --version`
  prints the new version.
* `brew install --cask tufantunc/tap/runtimepulse` (or `brew upgrade --cask
  runtimepulse`) works. The cask includes a quarantine-removal hook because
  binaries are unsigned; Gatekeeper will not block the install.
* `runtimepulse status` against a running old daemon prints the version
  mismatch warning (expected until the daemon restarts).
