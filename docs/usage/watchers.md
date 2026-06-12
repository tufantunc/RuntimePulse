# Watchers

A watch is a runtime condition observer hosted by the daemon. Watches are persisted: they survive daemon restarts (re-armed at boot) and terminal closure. "Zero polling" means the *agent* never polls — watchers use push APIs where they exist (fsnotify, `docker events`) and efficient internal probing where they don't.

## Common semantics

Every watch follows the same triggering contract:

1. **Initial check.** When a watch is created, the current state is measured immediately and emitted as an event. If the service is *already* up, `tcp.available` fires right away — the "it became ready before I started watching" deadlock is impossible.
2. **Edge triggering.** Afterwards, only state *transitions* emit events. A healthy service that stays healthy is silent.
3. **Flap suppression.** With `--stability 5s`, a new state must hold for 5s before its transition event is emitted. Default is no suppression.

Common flags: `--interval` (poll cadence for probing types, default `2s`) and `--stability`.

```bash
runtimepulse watch list          # JSON lines, one per watch
runtimepulse watch rm <watch-id>
```

Creating the same `(type, target)` twice is idempotent — you get the existing watch back.

## Watch types

### http — `--url`

```bash
runtimepulse watch add http --url http://localhost:3000 --interval 2s --stability 5s
```

Available = the target **itself** answers 2xx/3xx. Redirects are not followed (an app that 302s `/` → `/login` is up). Events: `http.available` / `http.unavailable`.

### tcp — `--addr`

```bash
runtimepulse watch add tcp --addr localhost:5432
```

The service-agnostic readiness check: available = the port accepts a TCP connection. The right tool for Postgres, Redis, or anything non-HTTP. Events: `tcp.available` / `tcp.unavailable`.

### file — `--path`

```bash
runtimepulse watch add file --path dist/index.js
```

fsnotify-based (no polling). Events: `file.created`, `file.changed`, `file.removed`. Bursts are coalesced (trailing-edge debounce, default 500ms — override with `--stability`): after a quiet period, **one** event reflecting the file's *final* state is emitted, so a bundler's delete-and-rewrite reports `file.changed`, never a stale `file.removed`. The parent directory may not exist yet (`dist/` before the first build) — the watch waits for it and survives the directory being removed and recreated.

### process — `--pattern`

```bash
runtimepulse watch add process --pattern "vite"
```

Running = `pgrep -f <pattern>` finds a match. Events: `process.started` / `process.exited`.

### docker — `--container`

```bash
runtimepulse watch add docker --container postgres
```

Push-based via `docker events` + `docker inspect` subprocesses (no Docker SDK; requires the `docker` CLI, validated at creation). Events: `docker.started`, `docker.healthy`, `docker.unhealthy`, `docker.stopped`. If the stream drops (docker daemon restart), the watch re-inspects and reattaches automatically.

### git — `--repo`

```bash
runtimepulse watch add git --repo .
```

fsnotify on `.git/HEAD` and refs. Events: `git.branch.changed` (payload: the new branch) and `git.ref.changed`. Pure edge — no initial event. There is deliberately no `git.pushed`: a push is not reliably detectable from the local machine.

## The `exec` wrapper — build & command outcomes

Builds, test runs, and migrations are not *watched* — they're *wrapped*. `exec` runs any command (inheriting your terminal), then converts its outcome into an event:

```bash
runtimepulse exec --label build -- npm run build
runtimepulse exec --label stack -- docker compose up -d --wait
```

* Exit 0 → `exec.succeeded`, nonzero → `exec.failed` — with the label as the event `source` and `exitCode`/`durationMs` in the payload.
* The child's exit code is preserved, so `exec` composes with `&&` in shell scripts and CI.
* `docker compose up --wait` blocks until containers are healthy, which makes `exec` the simplest "stack is ready" signal there is.

A rule on the failure side is the classic use:

```bash
runtimepulse rule add --on exec.failed:build --session <id> \
  --prompt 'The build failed (exit {{.Event.Payload.exitCode}}). Investigate and fix it.'
```

## Event taxonomy at a glance

| Watch type | Positive | Negative |
|---|---|---|
| http | `http.available` | `http.unavailable` |
| tcp | `tcp.available` | `tcp.unavailable` |
| docker | `docker.healthy`, `docker.started` | `docker.unhealthy`, `docker.stopped` |
| file | `file.created`, `file.changed` | `file.removed` |
| process | `process.started` | `process.exited` |
| exec wrapper | `exec.succeeded` | `exec.failed` |
| git | `git.branch.changed`, `git.ref.changed` | — |
| (internal) | `continuation.completed` | `continuation.failed` |

Failure events are first-class on purpose: the moments an agent most needs waking are the failures.
