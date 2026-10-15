#!/bin/sh
# Fails when GitHub cannot serve the archive of a pinned action commit (404/410). Runners with the action
# cached still pass, so a bad pin otherwise fails only some jobs, some of the time. A network error or a
# 5xx after retries says nothing about the pin, so it is reported and does not fail the gate.
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
  code=$("$CURL" -s -o /dev/null -w '%{http_code}' --retry 3 --retry-all-errors --connect-timeout 10 --max-time 60 \
    "https://codeload.github.com/$repo/tar.gz/$sha" || echo 000)
  case $code in
    200) ;;
    404 | 410) broken="$broken
$repo@$sha (HTTP $code)" ;;
    *) echo "warning: could not check $repo@$sha (HTTP $code); not counted as a broken pin" >&2 ;;
  esac
done

if [ -n "$broken" ]; then
  echo "GitHub cannot serve these pinned action archives; pin another release:$broken" >&2
  exit 1
fi
echo "every pinned action archive is fetchable"
