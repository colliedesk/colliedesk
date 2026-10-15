#!/bin/sh
# Fails when a job in ci.yml is missing from ci-ok's needs, so a new job can never fail on main
# while edge is still published. Jobs that themselves need ci-ok (publish-edge) are exempt.
# Usage: tests/ci-ok-needs-test.sh [path/to/ci.yml]
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKFLOW=${1:-$ROOT/.github/workflows/ci.yml}

missing=$(awk '
  /^jobs:/ { in_jobs = 1; next }
  in_jobs && /^[^ #]/ { in_jobs = 0 }
  in_jobs && /^  [A-Za-z0-9_-]+:[ ]*$/ { job = $1; sub(/:$/, "", job); jobs[job] = 1; order[++n] = job; next }
  in_jobs && /^    needs:/ {
    line = $0; sub(/^    needs:[ ]*/, "", line); gsub(/[][ ]/, "", line)
    needs[job] = line
  }
  END {
    split(needs["ci-ok"], gated, ",")
    for (i in gated) covered[gated[i]] = 1
    for (i = 1; i <= n; i++) {
      j = order[i]
      if (j == "ci-ok" || needs[j] == "ci-ok" || covered[j]) continue
      print j
    }
    if (!("ci-ok" in jobs)) print "(no ci-ok job)"
  }
' "$WORKFLOW")

if [ -n "$missing" ]; then
  printf 'ci-ok does not wait for: %s\n' "$(echo "$missing" | tr '\n' ' ')" >&2
  exit 1
fi
echo "ci-ok waits for every job"
