# Rules, Sessions & Continuations

This is RuntimePulse's core model. Three independent entities, one binding:

* a **Session** is an agent execution context you can resume,
* a **Rule** says *"when this event fires, resume that session with this prompt"*,
* a **Continuation** is one supervised resume — the record of it actually happening.

## Sessions

Register a session so the daemon knows which agent CLI to invoke and in which directory:

```bash
runtimepulse session register --agent claude --session <session-id> --repo ~/work/myapp
runtimepulse session list
```

* `--agent` is one of `claude`, `cursor`, `codex`, `opencode`.
* `--repo` matters: the resume command runs **in that directory**, which is how the agent gets back its project context (settings, permissions, MCP servers). Session id discovery per agent is documented in [`docs/reference/`](../reference/).
* Re-registering the same id updates agent/repo; it never duplicates.

The other registration path is [MCP self-registration](mcp.md) — the agent registers *itself* from inside its session, no manual id hunting.

## Rules

```bash
runtimepulse rule add \
  --on docker.healthy:postgres \
  --session <session-id> \
  --prompt 'Postgres ({{.Event.Source}}) is healthy. Continue the migration.' \
  --one-shot --label db-ready --expires 30m
```

| Flag | Meaning |
|---|---|
| `--on type[:source]` | Event selector. `docker.healthy:postgres` matches type+source; `docker.healthy` alone matches any source. |
| `--session` | Target session id (must be registered — or pass `--agent` + `--repo` to register it in the same call). |
| `--prompt` | Go `text/template` prompt — see below. |
| `--label` | Names the rule; becomes the `source` of its `continuation.completed/failed` result events (the chaining hook). |
| `--one-shot` | Consume the rule after its first match. Without it the rule fires on every matching event. |
| `--expires 30m` | TTL; expired rules never fire. `0` (default) = never. |

One event may trigger many rules (fan-out); one session may be bound by many rules. Manage with `rule list` (`--all` includes consumed/expired) and `rule rm <id>`.

### Prompt templates

The template context contains only machine-generated event fields — external free text never flows into an agent prompt:

```
{{.Event.Type}}      docker.healthy
{{.Event.Source}}    postgres
{{.Event.Payload.exitCode}}   payload values by key (field syntax)
```

Use **field syntax** for payload access (`{{.Event.Payload.container}}`): a referenced-but-missing key then fails loudly instead of silently injecting an empty string. Template *syntax* is validated when the rule is created; missing-field errors at event time produce a `failed` continuation carrying the error — a bad template never silently swallows a wake-up.

## Continuations

When an event matches a rule, a continuation is created transactionally with the event (at-least-once: a daemon crash can never lose the wake-up, only retry it). The dispatcher then:

1. claims it (`pending → running`),
2. runs the agent's resume command in the session's repo — supervised, 30-minute timeout,
3. records the outcome (`completed`/`failed`, exit code, command, output summary),
4. emits `continuation.completed` or `continuation.failed` back onto the event bus.

Per session, continuations run **strictly one at a time, FIFO** (agent CLIs can't parallel-resume one session); different sessions run in parallel. Inspect with:

```bash
runtimepulse continuations                    # everything
runtimepulse continuations --state failed     # with exit codes and output summaries
```

v1 resumes carry **no permission flags**: the agent runs under its own project-configured permissions, and headless runs deny unapproved tools — pre-approve what your workflow needs in the agent's own settings.

## Chaining

Continuation results are ordinary events whose `source` is the originating rule's **label**. A multi-step workflow is therefore nothing but rules:

```bash
runtimepulse rule add --on tcp.available:localhost:5432 --session $S \
  --prompt 'Postgres is up. Run the migrations.' --one-shot --label step-1

runtimepulse rule add --on continuation.completed:step-1 --session $S \
  --prompt 'Migrations done. Start the server and run the tests.' --one-shot --label step-2

runtimepulse rule add --on continuation.failed:step-1 --session $S \
  --prompt 'The migration step failed. Diagnose and retry.' --one-shot --label step-1-retry
```

There is no workflow engine — chaining is an emergent property of the event loop. For declaring chains in one file, see [Workflows](workflows.md).

> **Retry caution:** a rule on `continuation.failed` *without* `--one-shot` or `--expires` retries forever if the step keeps failing. Bound your retries.

## Manual resume

Poke a registered session without any event:

```bash
runtimepulse continue --session <session-id> --prompt "Status check: where are we?"
```

This enqueues a normal continuation (label `manual`) through the same FIFO/supervision pipeline.
