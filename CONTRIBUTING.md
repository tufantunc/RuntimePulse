# Contributing to RuntimePulse

Thanks for your interest in RuntimePulse. This document explains how to propose
changes and what the project expects from a contribution.

## Before you start

- **Questions and ideas** belong in [Discussions](https://github.com/tufantunc/RuntimePulse/discussions),
  not issues. Use issues for confirmed bugs and concrete feature requests.
- **Larger changes** start with a discussion or an issue first. RuntimePulse is
  designed deliberately — see [`docs/PROJECT.md`](docs/PROJECT.md) (the spec) and
  [`docs/superpowers/specs/`](docs/superpowers/specs/) (the rationale behind every
  architectural decision). Please read both before proposing structural changes,
  and flag in your issue if a decision recorded there needs revisiting.

## Development setup

RuntimePulse is a single Go module (Go 1.26+). No CGO; the SQLite driver is
`modernc.org/sqlite`.

```bash
git clone https://github.com/tufantunc/RuntimePulse.git
cd RuntimePulse
go build ./...
```

## The checks your change must pass

Run all of these locally before opening a PR — CI runs the same on a 3-OS matrix:

```bash
go test -race ./...                                  # unit + integration tests
go vet ./...
gofmt -l .                                           # must print nothing
golangci-lint run                                    # if installed
./scripts/smoke.sh                                   # hermetic end-to-end; must end "SMOKE OK"
GOOS=windows go build ./... && GOOS=windows go vet ./...   # the Windows gate
```

The Windows gate is required for **every** change: AF_UNIX is used on all
platforms, but platform-specific behavior lives behind build tags. Actual
Windows test execution happens only in CI.

## How we work

- **Test-driven.** New behavior comes with tests written first. Follow the
  patterns already in the package you're touching.
- **Small, focused commits** with clear messages. We use a
  `type(scope): summary` convention (e.g. `feat(setup): …`, `fix(adapter): …`,
  `test(smoke): …`, `docs: …`).
- **One concern per PR.** Don't fold unrelated refactors into a feature.
- **Documentation in English**, including code comments and docs.

### Some load-bearing conventions (so a PR isn't surprised in review)

- The store uses a **single SQLite connection**; never touch `s.db` while a
  transaction or rows are open.
- Prompts are rendered at ingest and stored on the continuation — the dispatcher
  reads `c.Prompt` and never re-renders.
- In `scripts/smoke.sh`, never pipe a command into `grep -q` under
  `set -o pipefail` (it SIGPIPEs the producer); use the `has`/`count`
  capture-then-match helpers.
- Test mocks for external binaries use the `internal/mockexe` re-exec pattern —
  never write `#!/bin/sh` mocks in Go tests.

## Submitting a pull request

1. Fork and branch from `main` (`feat/…`, `fix/…`, `docs/…`).
2. Make your change with tests; run the full check list above.
3. Open the PR using the template. Describe what changed and how you verified it.
4. CI must be green. A maintainer will review.

## Reporting security issues

Do **not** open a public issue for security problems. See [SECURITY.md](SECURITY.md).

## License

By contributing, you agree that your contributions are licensed under the
project's [Apache License 2.0](LICENSE).
