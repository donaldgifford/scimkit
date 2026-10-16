#!/usr/bin/env bash
# Per-package statement-coverage gate (IMPL-0001 Q6 decision a): library
# packages must hold the floor; cmd/ binaries, the internal/schemagen
# go:generate tool, and generated files are exempt. Reads an existing
# coverage profile (arg 1, default coverage.out) or generates one.
#
# Caveat: packages with zero test files never appear in a coverage profile,
# so they are reported as warnings rather than silently passing.
set -euo pipefail

PROFILE="${1:-coverage.out}"
FLOOR="${COVERAGE_FLOOR:-80}"

if [[ ! -f "$PROFILE" ]]; then
  echo "==> No $PROFILE found; generating coverage profile"
  go test -coverprofile="$PROFILE" ./... >/dev/null
fi

echo "==> Per-package coverage (floor ${FLOOR}%)"

per_package="$(awk '
NR > 1 {
  split($1, loc, ":")
  file = loc[1]
  if (file ~ /_gen\.go$/) next          # generated code exempt
  if (file ~ /\/cmd\//) next            # binaries exempt
  if (file ~ /internal\/schemagen\//) next  # go:generate dev tool exempt
  pkg = file
  sub(/\/[^\/]+$/, "", pkg)
  stmts[pkg] += $2
  if ($3 > 0) covered[pkg] += $2
}
END {
  for (pkg in stmts) {
    if (stmts[pkg] == 0) continue
    printf "%s %.1f\n", pkg, 100 * covered[pkg] / stmts[pkg]
  }
}' "$PROFILE" | sort)"

status=0
while read -r pkg pct; do
  [[ -z "$pkg" ]] && continue
  if awk -v p="$pct" -v f="$FLOOR" 'BEGIN { exit !(p < f) }'; then
    printf '  ✗ %-58s %6s%%  below floor\n' "$pkg" "$pct"
    status=1
  else
    printf '  ✓ %-58s %6s%%\n' "$pkg" "$pct"
  fi
done <<<"$per_package"

# Library packages with no tests at all never show up in the profile; call
# them out so new packages cannot dodge the gate unnoticed.
while read -r pkg; do
  [[ -z "$pkg" ]] && continue
  case "$pkg" in
  */cmd/* | */internal/schemagen) continue ;;
  esac
  printf '  ! %-58s  no test files (not gated yet)\n' "$pkg"
done < <(go list -f '{{if and (not .TestGoFiles) (not .XTestGoFiles)}}{{.ImportPath}}{{end}}' ./...)

if [[ "$status" -ne 0 ]]; then
  echo "✗ Coverage gate failed (floor ${FLOOR}%)"
  exit 1
fi
echo "✓ Coverage gate passed"
