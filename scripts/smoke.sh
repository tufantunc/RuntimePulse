#!/usr/bin/env bash
# Smoke test: daemon up → session registered → rule added → event injected
# → pending continuation exists → follow stream works → idempotent re-inject.
set -euo pipefail

dir=$(mktemp -d /tmp/rp-smoke.XXXXXX)
trap 'kill $dpid 2>/dev/null || true; rm -rf "$dir"' EXIT
export RUNTIMEPULSE_DIR="$dir"

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

status=$("$rp" status)
echo "$status"
echo "$status" | grep -q '"pendingContinuations":1' || { echo "FAIL: expected 1 pending continuation"; exit 1; }

# oneShot: same event again must not create a second continuation
"$rp" inject --type docker.healthy --source postgres
status=$("$rp" status)
echo "$status" | grep -q '"pendingContinuations":1' || { echo "FAIL: oneShot consumed twice"; exit 1; }

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
sleep 0.8
status=$("$rp" status)
echo "$status"
echo "$status" | grep -q '"pendingContinuations":2' || { echo "FAIL: file watch did not fire rule"; exit 1; }

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

echo "SMOKE OK"
