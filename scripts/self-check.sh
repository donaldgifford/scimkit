#!/usr/bin/env bash
# Simulator ↔ mock self-check (IMPL-0001 phase 6): the simulator validates
# the mock, and an intentionally broken configuration proves the simulator
# detects real problems. Uses the built CLI (arg 1, default
# build/bin/scimkit) end to end over a real TCP listener.
set -euo pipefail

BIN="${1:-build/bin/scimkit}"
ADDR="${SCIMKIT_ADDR:-127.0.0.1:18080}"
TOKEN="self-check-token"
SCENARIO="rfc-strict-lifecycle"

[[ -x "$BIN" ]] || { echo "self-check: $BIN not found; run 'just build'" >&2; exit 1; }

"$BIN" mock --addr "$ADDR" --token "$TOKEN" &
MOCK_PID=$!
cleanup() {
  kill -TERM "$MOCK_PID" 2>/dev/null || true
  wait "$MOCK_PID" 2>/dev/null || true
}
trap cleanup EXIT

for _ in $(seq 1 30); do
  if curl -fsS "http://${ADDR}/ServiceProviderConfig" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

echo "==> self-check: healthy mock must pass ${SCENARIO}" >&2
"$BIN" exercise --target "http://${ADDR}" --token "$TOKEN" --scenario "$SCENARIO"

echo "==> self-check: wrong token must fail ${SCENARIO}" >&2
if "$BIN" exercise --target "http://${ADDR}" --token "wrong-token" --scenario "$SCENARIO"; then
  echo "FAIL: simulator passed against a broken configuration" >&2
  exit 1
fi

echo "==> self-check: Okta-shaped HTTP flow" >&2
"$(dirname "$0")/smoke-http.sh" "http://${ADDR}" "$TOKEN"

echo "==> self-check passed" >&2
