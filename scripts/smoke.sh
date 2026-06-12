#!/usr/bin/env bash
# Smoke test: daemon up → session registered → rule added → event injected
# → continuation dispatched (mock claude) → chaining → manual continue.
set -euo pipefail

dir=$(mktemp -d /tmp/rp-smoke.XXXXXX)
trap 'kill $dpid 2>/dev/null || true; rm -rf "$dir"' EXIT
export RUNTIMEPULSE_DIR="$dir"

# Install a mock claude binary so every daemon in this script dispatches
# continuations hermetically without the real claude CLI.
cat > "$dir/mock-claude" << 'MOCK'
#!/bin/sh
# stand-in for the claude CLI: prints structured output like
# `claude -p --output-format json` and exits 0
echo '{"result":"mock turn done","session_id":"'$2'"}'
MOCK
chmod +x "$dir/mock-claude"
export RUNTIMEPULSE_CLAUDE_BIN="$dir/mock-claude"

go build -o "$dir/runtimepulse" ./cmd/runtimepulse
rp="$dir/runtimepulse"

"$rp" daemon >"$dir/daemon-out.log" 2>&1 &
dpid=$!
sleep 0.5

"$rp" session register --agent claude --session smoke-1 --repo "$dir"
"$rp" rule add --on docker.healthy:postgres --session smoke-1 \
  --prompt 'Postgres ({{.Event.Source}}) is healthy. Continue.' --one-shot --label step-1

"$rp" events --follow >"$dir/follow.jsonl" &
fpid=$!
sleep 0.3

"$rp" inject --type docker.healthy --source postgres --payload container=postgres

# The dispatcher picks up continuations immediately; pending counts are transient.
# Assert via total continuation rows for the session instead.
sleep 1
smoke1_total=$("$rp" continuations | grep -c smoke-1)
[ "$smoke1_total" -eq 1 ] || { echo "FAIL: expected 1 continuation for smoke-1, got $smoke1_total"; exit 1; }

# oneShot: same event again must not create a second continuation
"$rp" inject --type docker.healthy --source postgres
smoke1_total=$("$rp" continuations | grep -c smoke-1)
[ "$smoke1_total" -eq 1 ] || { echo "FAIL: oneShot consumed twice, smoke-1 got $smoke1_total continuations"; exit 1; }

sleep 0.3
kill $fpid 2>/dev/null || true
grep -q 'docker.healthy' "$dir/follow.jsonl" || { echo "FAIL: follow stream empty"; exit 1; }

# --- watchers: file watch fires a rule via a real fs event ---
"$rp" session register --agent claude --session smoke-2 --repo "$dir"
"$rp" rule add --on file.created:"$dir/artifact.txt" --session smoke-2 \
  --prompt 'Artifact ready. Continue.' --one-shot
"$rp" watch add file --path "$dir/artifact.txt"
sleep 0.3
touch "$dir/artifact.txt"
sleep 1.2
smoke2_total=$("$rp" continuations | grep -c smoke-2)
[ "$smoke2_total" -eq 1 ] || { echo "FAIL: file watch did not fire rule (smoke-2 continuations: $smoke2_total)"; exit 1; }

# --- exec wrapper: success and failure both produce events ---
"$rp" exec --label smoke-build -- true
if "$rp" exec --label smoke-build -- false; then echo "FAIL: exec must preserve exit code"; exit 1; fi
"$rp" events --type exec.succeeded | grep -q smoke-build || { echo "FAIL: exec.succeeded missing"; exit 1; }
"$rp" events --type exec.failed | grep -q smoke-build || { echo "FAIL: exec.failed missing"; exit 1; }

# --- watch survives daemon restart (re-arm) ---
"$rp" watch list | grep -q '"type":"file"' || { echo "FAIL: watch not persisted"; exit 1; }
kill $dpid && wait $dpid 2>/dev/null || true
"$rp" daemon >"$dir/daemon-out2.log" 2>&1 &
dpid=$!
sleep 0.5
"$rp" watch list | grep -q '"type":"file"' || { echo "FAIL: watch lost after restart"; exit 1; }

# --- dispatcher: event → resume (mock claude) → chained second step ---
"$rp" session register --agent claude --session smoke-3 --repo "$dir"
"$rp" rule add --on tcp.available:smoke-tcp --session smoke-3 \
  --prompt 'TCP up. Do step one.' --one-shot --label chain-1
"$rp" rule add --on continuation.completed:chain-1 --session smoke-3 \
  --prompt 'Step one done. Do step two.' --one-shot --label chain-2
"$rp" inject --type tcp.available --source smoke-tcp
# Poll for the chain-2 completion EVENT, not the continuation count:
# a continuation is marked completed slightly before its
# continuation.completed event is emitted, so the event is the true
# end-of-chain signal. Its presence implies both hops ran.
for _ in $(seq 1 50); do
  "$rp" events --type continuation.completed | grep -q chain-2 && break
  sleep 0.2
done
"$rp" events --type continuation.completed | grep -q chain-2 || { echo "FAIL: chain-2 completion event missing"; exit 1; }
completed=$("$rp" continuations --state completed | grep -c smoke-3 || true)
[ "$completed" -eq 2 ] || { echo "FAIL: chain expected 2 completed continuations for smoke-3, got $completed"; exit 1; }

# --- manual continue ---
"$rp" continue --session smoke-3 --prompt "manual poke"
for _ in $(seq 1 25); do
  "$rp" continuations --state completed | grep -q '"label":"manual"' && break
  sleep 0.2
done
"$rp" continuations --state completed | grep -q '"label":"manual"' || { echo "FAIL: manual continuation missing"; exit 1; }

# --- MCP server: stdio handshake lists the five tools ---
mcp_in="$dir/mcp_in.jsonl"
cat > "$mcp_in" << 'JSONL'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
JSONL
# Keep stdin open briefly after the requests so the server flushes the
# tools/list response before EOF closes the stream.
mcp_out=$( (cat "$mcp_in"; sleep 1) | "$rp" mcp 2>/dev/null || true)
for tool in create_watch create_rule wait_for_event get_events cancel_rule; do
  echo "$mcp_out" | grep -q "$tool" || { echo "FAIL: MCP tools/list missing $tool"; exit 1; }
done

echo "SMOKE OK"
