#!/bin/sh
# Tests tests/pinned-actions-fetchable-test.sh with a fake curl: a missing archive (404/410) fails the gate;
# a network error or 5xx after retries is reported but does not fail it (it proves nothing about the pin).
set -eu
HERE=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/repo/.github/workflows"
printf 'jobs:\n  a:\n    steps:\n      - uses: actions/checkout@%s # v7\n' 3d3c42e5aac5ba805825da76410c181273ba90b1 >"$WORK/repo/.github/workflows/ci.yml"
failures=0
expect() { # name, fake http code, want exit
  printf '#!/bin/sh\nprintf %s\n' "$2" >"$WORK/curl"; chmod +x "$WORK/curl"
  set +e; CURL="$WORK/curl" sh "$HERE/pinned-actions-fetchable-test.sh" "$WORK/repo" >/dev/null 2>&1; got=$?; set -e
  if [ "$got" -ne "$3" ]; then echo "FAIL $1: exit $got, want $3"; failures=$((failures + 1)); fi
}
expect 'archive served' 200 0
expect 'archive missing' 404 1
expect 'archive gone' 410 1
expect 'network error' 000 0
expect 'codeload down' 503 0
[ "$failures" -eq 0 ] && echo "fetchable gate tests passed" || exit 1
