# WebSocket Event Stream

An opt-in live event stream for external consumers — dashboards, log collectors, anything outside the unix socket. **Off by default**; the daemon's only listener is the unix socket unless you ask otherwise.

## Enabling

```bash
runtimepulse daemon --ws-port 8787
```

The endpoint is `ws://127.0.0.1:8787/events`. The bind address is **hardcoded to loopback** — it cannot be exposed on a network interface by configuration, by design (spec §9).

## Authentication

A token is generated on first use and persisted at `~/.runtimepulse/ws-token` (mode 0600, stable across restarts). Pass it either way:

```
ws://127.0.0.1:8787/events?token=<token>
```

or as a header: `Authorization: Bearer <token>`. Wrong or missing token → HTTP 401.

## Consuming

Each message is one event as JSON — the same shape as `runtimepulse events` output:

```json
{"id":"evt-…","type":"continuation.completed","source":"db-ready","payload":{"sessionId":"abc123","exitCode":"0","durationMs":"41230"},"timestamp":"…"}
```

Example with [websocat](https://github.com/vi/websocat):

```bash
websocat "ws://127.0.0.1:8787/events?token=$(cat ~/.runtimepulse/ws-token)"
```

## Delivery semantics

The stream is a live bus subscription, same as `events --follow`:

* **No replay.** You see events published after you connect.
* **Slow consumers miss events** (bounded buffer, no gap signal). Durability lives in the store — reconcile with `runtimepulse events` / the `get_events` MCP tool when completeness matters.
* One connection per consumer; reconnect freely, the token is stable.

For most automation you don't want this at all — [rules](rules-and-sessions.md) *are* the push mechanism. The stream exists for humans and dashboards watching the system work.
