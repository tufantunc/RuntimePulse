# Workflows

Multi-step chains can be declared in one YAML file instead of assembled rule-by-rule. **There is no workflow entity in the core** — `runtimepulse apply` merely compiles the file into ordinary one-shot [rules](rules-and-sessions.md), chained through `continuation.completed` events. What you apply is exactly what `rule list` shows.

## Format

```yaml
# migrate.yaml
session: abc123          # required: the session to resume
agent: claude            # required: claude | cursor | codex | opencode
repo: /Users/me/work/app # optional: session repo; defaults to the directory you run `apply` in

steps:
  - label: db-ready
    on: { type: tcp.available, source: "localhost:5432" }
    prompt: "Postgres is up. Run the migrations."

  - label: migrated      # label defaults to step-N when omitted
    on: { type: continuation.completed, source: db-ready }
    prompt: "Migrations done. Start the server and run the tests."

  - on: { type: continuation.failed, source: db-ready }
    prompt: "The migration step failed (see logs). Diagnose and fix."
```

* Each step compiles to a **one-shot** rule: a workflow is one pass. Re-`apply` the file to re-arm it.
* `on.source` referencing a previous step's `label` is the chaining mechanism; `on` can equally match watcher events (`docker.healthy`, `exec.failed`, …).
* Prompts are Go `text/template` over the event (`{{.Event.Source}}`, `{{.Event.Payload.key}}`) — syntax-checked at parse time.
* Unknown YAML fields are **errors** (typo protection), as are missing `session`/`agent`/`type`/`prompt` and duplicate labels.

## Applying

```bash
runtimepulse apply migrate.yaml
# one JSON line per created rule
```

The first rule self-registers the session (agent + repo), so a `session register` beforehand is unnecessary. On a partial failure, already-created rules are removed (best-effort rollback) — a bad file never leaves half a workflow armed. Arm the watches the first step needs separately:

```bash
runtimepulse watch add tcp --addr localhost:5432
```

## A complete example: the acceptance scenario

Database up → migrate → test → report, with zero agent-side polling:

```yaml
session: abc123
agent: claude
steps:
  - label: stack-ready
    on: { type: exec.succeeded, source: stack }
    prompt: "The compose stack is healthy. Run the database migrations."
  - label: migrated
    on: { type: continuation.completed, source: stack-ready }
    prompt: "Migrations applied. Start the dev server and run the test suite."
  - label: report
    on: { type: continuation.completed, source: migrated }
    prompt: "Tests finished. Summarize the results and any failures."
```

```bash
runtimepulse apply pipeline.yaml
runtimepulse exec --label stack -- docker compose up -d --wait   # kicks off the chain
runtimepulse events --follow                                     # watch it run
```

## Cleaning up

A workflow is just rules — `runtimepulse rule list` / `rule rm <id>` manages or cancels it. Consumed one-shot rules are visible with `rule list --all`.
