# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities **privately**. Do not open a public issue.

- Preferred: use GitHub's [private vulnerability reporting](https://github.com/tufantunc/RuntimePulse/security/advisories/new)
  ("Report a vulnerability" under the Security tab).
- Alternatively, email **tufan.tunc.91@gmail.com** with details and, if possible,
  a minimal reproduction.

You can expect an initial acknowledgement within a few days. Please give us a
reasonable window to investigate and ship a fix before any public disclosure.

## Supported versions

RuntimePulse is pre-1.x-stability in spirit despite its version numbers: only the
**latest released version** receives security fixes. Please upgrade before
reporting (`brew upgrade --cask runtimepulse`, or re-run the install script).

## Security model (what to keep in mind when reviewing)

RuntimePulse is a local-first daemon. Its trust boundaries:

- **Unix socket only, mode 0600.** The daemon's control plane (CLI + MCP server)
  speaks JSON-RPC over a unix socket in `~/.runtimepulse/`, never over TCP. There
  is no network listener by default.
- **Opt-in WebSocket stream is loopback-only and token-gated.** `--ws-port` binds
  `127.0.0.1` only; consumers must present a token persisted at
  `~/.runtimepulse/ws-token` (mode 0600). It is a read-only event stream.
- **Resumes run the agent's own CLI** in the session's registered repo directory,
  with **no permission/sandbox flags** added by RuntimePulse (a deliberate v1
  decision). The resumed agent operates under whatever permissions you configured
  for it. Treat rule prompts as something that will run in your agent's context.
- **Prompt templates** are Go `text/template`, and only machine-generated event
  fields enter template context — free-form external text never flows into a
  template.

When reporting, it helps to frame the issue against one of these boundaries
(e.g. "an unprivileged local process can …", "a watcher payload can inject …").
