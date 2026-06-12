# Retention Sweeper — Design

**Date:** 2026-06-12
**Status:** Approved

## Goal

The daemon automatically garbage-collects accumulated state so a long-running install never grows unbounded — with **no agent or LLM involvement**, purely the daemon's own clock. Three things accrete today with no cleanup path:

* **Sessions** — every `session register` (manual or MCP self-registration) adds a row that is never removed. Heavy MCP use registers a new session per agent session; they pile up forever.
* **Continuations** — terminal (`completed`/`failed`) records accumulate.
* **Events** — `Store.PruneEvents` exists but is **never called**; the design's promised event retention was never actually wired up. This is the moment to fix that.

There is no `session rm` and no pruning command; the fix is structural, not manual.

## Decisions (owner-approved)

* **Mechanism:** a single periodic retention sweep in the daemon — not per-agent liveness probing. Liveness probing (asking each agent CLI "does this session still exist?") was rejected: slow, fragile, per-agent subprocesses, CLIs drift. Age + active-rule gating is cheap (indexed DELETEs) and robust.
* **Session GC gate (three conditions, all required):** delete a session only when `updated_at < now − 7d` **AND** it is not referenced by any *active* rule (a rule that is not consumed and not expired) **AND** its state is not `running`. An armed-and-waiting session (has an active rule) is never deleted regardless of age; an in-flight session (`running`, or freshly resumed → `updated_at` bumped) is never deleted.
* **Empty-but-young sessions are kept:** a session with no active rule but registered recently is *not* deleted until the 7-day window elapses — preserves the "register now, add a rule (or manual `continue`) shortly after" flow. The gate is age-first.
* **Retention windows are fixed constants (no flags, YAGNI):** session idle 7d, terminal continuation 30d, event 30d. Configurability can be added later if needed.

## Architecture

### `internal/store/retention.go`

One method runs all three deletes in **one transaction** (consistent with the outbox pattern; the single-connection store forbids overlapping calls, so all DELETEs run sequentially on the same `*sql.Tx`):

```go
type RetentionPolicy struct {
    SessionIdle     time.Duration // 7d
    ContinuationAge time.Duration // 30d
    EventAge        time.Duration // 30d
}

type SweepResult struct {
    Sessions      int64
    Continuations int64
    Events        int64
}

func (s *Store) Sweep(now time.Time, p RetentionPolicy) (SweepResult, error)
```

DELETEs (UTC, fixed-width `ts()` strings so lexicographic = chronological, as everywhere):

* **Sessions** — gated:
  ```sql
  DELETE FROM sessions
  WHERE updated_at < ?            -- now − SessionIdle
    AND state != 'running'
    AND session_id NOT IN (
      SELECT session_id FROM rules
      WHERE consumed = 0 AND (expires_at IS NULL OR expires_at > ?)   -- now
    )
  ```
* **Continuations** — terminal + old only (pending/running never touched):
  ```sql
  DELETE FROM continuations
  WHERE state IN ('completed','failed') AND updated_at < ?            -- now − ContinuationAge
  ```
* **Events** — fold the existing `PruneEvents` predicate into the same tx:
  ```sql
  DELETE FROM events WHERE created_at < ?                              -- now − EventAge
  ```

`PruneEvents` stays (it has tests and is a fine standalone), but the scheduled path goes through `Sweep`. `RowsAffected` for each populates `SweepResult`.

### `internal/daemon`

A `retentionSweeper` started in `Serve`, mirroring the dispatcher's lifecycle discipline:

* Runs `Sweep` once at boot, then on a `time.NewTicker(1h)`.
* `defer ticker.Stop()`; selects on `ctx.Done()` to exit cleanly; the daemon's shutdown path waits for nothing extra (the sweep is a quick indexed transaction, not a long-running agent run).
* Logs a single line only when something was removed: `retention sweep: sessions=N continuations=M events=K`. A failed sweep logs the error and continues (never fatal; next tick retries).
* Policy constants live in the daemon (`sessionIdle = 7 * 24 * time.Hour`, etc.).

Three independent prunes, **no cascade:** deleting a session does not delete its historical continuations — those are pruned by their own age. A continuation referencing an already-deleted session is inert (terminal state; the dispatcher never re-runs it). Keeping the three prunes independent keeps each query and its tests simple.

## Testing

**Store (`retention_test.go`), the behavioral core:**
* Session matrix: old+no-active-rule+waiting → deleted; old+active-rule → kept; old+running → kept; young+no-rule → kept (age gate); old+expired-rule-only → deleted (expired rule is not active); old+consumed-oneShot-rule → deleted.
* Continuations: old completed/failed → deleted; old pending/running → kept; young terminal → kept.
* Events: old → deleted; young → kept.
* `SweepResult` counts match the deletions; a second immediate `Sweep` deletes nothing (idempotent); all in one tx (a mid-sweep error leaves nothing partially deleted — exercised by construction).

**Daemon (`retention_test.go`):**
* With a pre-seeded old, rule-less session and a short test sweep interval (inject via a small seam, or call `d.sweepOnce()` directly), assert the session is gone after a sweep and the log/counts reflect it. A session with an active rule survives.

**Smoke:** out of scope — the sweep's behavior is time-based (7d/30d windows) and fully covered by store unit tests; smoke would need clock injection it doesn't have. (If a quick check is wanted, `inject` an old event is impossible via CLI since timestamps are server-set; skip.)

## Docs

* `docs/PROJECT.md` §8 (Storage): replace the "events pruned … (configurable retention)" aspiration with the real behavior — a daemon retention sweep (hourly) GCs idle sessions (7d, only when no active rule references them), terminal continuations (30d), and events (30d).
* `docs/usage/getting-started.md`: one line under state/inspection noting sessions are auto-pruned when idle and unreferenced, so the registry stays clean without manual `session rm`.

## Out of scope (deliberate)

* Manual `session rm <id>` / `prune` commands — the ask was *automatic* cleanup; a manual escape hatch is a separate small addition if wanted.
* Per-agent liveness probing.
* Configurable retention windows (flags).
* Cascading session deletion into its continuations (kept independent by design).
