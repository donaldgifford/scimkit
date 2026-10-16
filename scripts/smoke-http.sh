#!/usr/bin/env bash
# HTTP smoke test for a running scimkit mock (IMPL-0001 phase 6): drives an
# Okta-shaped create → lookup → patch flow against BASE_URL (arg 1) and
# asserts each hop with curl + jq. Used by the CI container smoke job and
# `just smoke`; exits non-zero on the first failed assertion.
set -euo pipefail

BASE_URL="${1:?usage: smoke-http.sh BASE_URL [TOKEN]}"
TOKEN="${2:-}"
CT="application/scim+json"
USER_NAME="smoke.$(date +%s)@example.com"

auth=()
if [[ -n "$TOKEN" ]]; then
  auth=(-H "Authorization: Bearer ${TOKEN}")
fi

# request METHOD PATH [BODY] → prints "STATUS\nBODY"
request() {
  local method="$1" path="$2" body="${3:-}"
  local args=(-sS -X "$method" -H "Accept: ${CT}" "${auth[@]}" -w '\n%{http_code}')
  if [[ -n "$body" ]]; then
    args+=(-H "Content-Type: ${CT}" --data "$body")
  fi
  curl "${args[@]}" "${BASE_URL}${path}"
}

# expect NAME WANT_STATUS RESPONSE → prints body, fails on status mismatch
expect() {
  local name="$1" want="$2" resp="$3"
  local status body
  status="$(tail -n1 <<<"$resp")"
  body="$(sed '$d' <<<"$resp")"
  if [[ "$status" != "$want" ]]; then
    echo "FAIL ${name}: want HTTP ${want}, got ${status}" >&2
    echo "$body" >&2
    exit 1
  fi
  echo "ok   ${name} (${status})" >&2
  printf '%s' "$body"
}

echo "==> smoke: ${BASE_URL}" >&2

# Wait for the server to answer discovery (container may still be starting).
for _ in $(seq 1 30); do
  if curl -fsS "${auth[@]}" "${BASE_URL}/ServiceProviderConfig" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

spc="$(expect "GET /ServiceProviderConfig" 200 "$(request GET /ServiceProviderConfig)")"
jq -e '.patch.supported == true and .filter.supported == true' <<<"$spc" >/dev/null \
  || { echo "FAIL discovery: patch/filter not advertised" >&2; exit 1; }

# Okta pre-create lookup: userName eq → empty ListResponse, not an error.
filter="$(jq -rn --arg u "$USER_NAME" '"userName eq \"\($u)\"" | @uri')"
lookup="$(expect "GET /Users?filter (pre-create)" 200 "$(request GET "/Users?filter=${filter}")")"
jq -e '.totalResults == 0' <<<"$lookup" >/dev/null \
  || { echo "FAIL pre-create lookup: expected totalResults 0" >&2; exit 1; }

create_body="$(jq -cn --arg u "$USER_NAME" '{
  schemas: ["urn:ietf:params:scim:schemas:core:2.0:User"],
  userName: $u,
  name: {givenName: "Smoke", familyName: "Test"},
  emails: [{value: $u, primary: true, type: "work"}],
  active: true
}')"
created="$(expect "POST /Users" 201 "$(request POST /Users "$create_body")")"
id="$(jq -r '.id' <<<"$created")"
[[ -n "$id" && "$id" != "null" ]] || { echo "FAIL create: no id" >&2; exit 1; }

lookup="$(expect "GET /Users?filter (post-create)" 200 "$(request GET "/Users?filter=${filter}")")"
jq -e --arg id "$id" '.totalResults == 1 and .Resources[0].id == $id' <<<"$lookup" >/dev/null \
  || { echo "FAIL post-create lookup: user not found by userName" >&2; exit 1; }

# Okta deactivation: PATCH active=false, server must answer 200 + body.
patch_body='{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","value":{"active":false}}]}'
patched="$(expect "PATCH /Users/{id}" 200 "$(request PATCH "/Users/${id}" "$patch_body")")"
jq -e '.active == false' <<<"$patched" >/dev/null \
  || { echo "FAIL patch: active not false" >&2; exit 1; }

fetched="$(expect "GET /Users/{id}" 200 "$(request GET "/Users/${id}")")"
jq -e '.active == false' <<<"$fetched" >/dev/null \
  || { echo "FAIL get: active not persisted" >&2; exit 1; }

expect "DELETE /Users/{id}" 204 "$(request DELETE "/Users/${id}")" >/dev/null
expect "GET /Users/{id} (deleted)" 404 "$(request GET "/Users/${id}")" >/dev/null

echo "==> smoke passed" >&2
