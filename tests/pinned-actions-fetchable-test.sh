#!/bin/sh
# Fails when GitHub cannot serve the archive of a pinned action commit. Runners with the action
# cached still pass, so a bad pin otherwise fails only some jobs, some of the time.
# Usage: tests/pinned-actions-fetchable-test.sh [repo root]
set -eu

ROOT=${1:-$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)}
CURL=${CURL:-curl}

pins=$(grep -hoE 'uses:[[:space:]]+[^[:space:]./][^[:space:]@]*@[0-9a-f]{40}' "$ROOT"/.github/workflows/*.yml \
  | sed -E 's/^uses:[[:space:]]+//' | awk -F@ '{ split($1, p, "/"); print p[1] "/" p[2] "@" $2 }' | sort -u)

broken=
for pin in $pins; do
  repo=${pin%@*}
  sha=${pin#*@}
  code=$("$CURL" -s -o /dev/null -w '%{http_code}' --retry 2 "https://codeload.github.com/$repo/tar.gz/$sha" || echo 000)
  [ "$code" = 200 ] || broken="$broken
$repo@$sha (HTTP $code)"
done

if [ -n "$broken" ]; then
  echo "GitHub cannot serve these pinned action archives; pin another release:$broken" >&2
  exit 1
fi
echo "every pinned action archive is fetchable"
