#!/bin/sh
# Fails when GitHub says the archive of a pinned action commit does not exist. Runners with the
# action cached still pass, so a bad pin otherwise fails only some jobs, some of the time.
# Throttling (429), server errors and network failures say nothing about the pin: they are retried
# with backoff and, if they never clear, reported without failing the build.
# Usage: tests/pinned-actions-fetchable-test.sh [repo root]
set -eu

ROOT=${1:-$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)}
CURL=${CURL:-curl}
SLEEP=${SLEEP:-sleep}
ATTEMPTS=5

pins=$(grep -hoE 'uses:[[:space:]]+[^[:space:]./][^[:space:]@]*@[0-9a-f]{40}' "$ROOT"/.github/workflows/*.yml \
  | sed -E 's/^uses:[[:space:]]+//' | awk -F@ '{ split($1, p, "/"); print p[1] "/" p[2] "@" $2 }' | sort -u)

broken=
unchecked=
for pin in $pins; do
  repo=${pin%@*}
  sha=${pin#*@}
  attempt=1
  while :; do
    code=$("$CURL" -s -o /dev/null -w '%{http_code}' "https://codeload.github.com/$repo/tar.gz/$sha" || echo 000)
    case $code in
      200) break ;;
      4[0-9][0-9]) [ "$code" = 429 ] || { broken="$broken
$repo@$sha (HTTP $code)"; break; } ;;
    esac
    if [ "$attempt" -ge "$ATTEMPTS" ]; then
      unchecked="$unchecked
$repo@$sha (HTTP $code)"
      break
    fi
    "$SLEEP" $((attempt * 10))
    attempt=$((attempt + 1))
  done
done

if [ -n "$broken" ]; then
  echo "GitHub cannot serve these pinned action archives; pin another release:$broken" >&2
  exit 1
fi
if [ -n "$unchecked" ]; then
  echo "warning: these pinned action archives could not be checked (GitHub throttled or unreachable):$unchecked" >&2
  exit 0
fi
echo "every pinned action archive is fetchable"
