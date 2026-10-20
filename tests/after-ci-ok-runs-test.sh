#!/bin/sh
# Fails when a job that needs ci-ok can be skipped by a skipped job further up. GitHub skips a
# job whose ancestor was skipped unless its `if:` uses a status function, and osv-scan is
# skipped on every push to main, so such a job must test ci-ok's result itself.
# Usage: tests/after-ci-ok-runs-test.sh [path/to/ci.yml]
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKFLOW=${1:-$ROOT/.github/workflows/ci.yml}

bad=$(awk '
  /^jobs:/ { in_jobs = 1; next }
  in_jobs && /^[^ #]/ { in_jobs = 0 }
  in_jobs && /^  [A-Za-z0-9_-]+:[ ]*$/ { job = $1; sub(/:$/, "", job); order[++n] = job; next }
  in_jobs && /^    needs:[ ]*ci-ok[ ]*$/ { after[job] = 1 }
  in_jobs && /^    if:/ { cond[job] = $0 }
  END {
    for (i = 1; i <= n; i++) {
      j = order[i]
      if (!after[j]) continue
      if (index(cond[j], "!cancelled()") && index(cond[j], "needs.ci-ok.result == '\''success'\''")) continue
      print j
    }
  }
' "$WORKFLOW")

if [ -n "$bad" ]; then
  printf 'jobs after ci-ok must use if: !cancelled() && needs.ci-ok.result == '\''success'\'': %s\n' "$(echo "$bad" | tr '\n' ' ')" >&2
  exit 1
fi
echo "jobs after ci-ok run whenever ci-ok passed"
