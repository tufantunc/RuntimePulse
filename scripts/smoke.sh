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

echo "SMOKE OK"
