#!/bin/sh
# Checks .github/scripts/ci-background.sh: start returns while the command runs, wait passes on its
# output and exit status, and a command that never finishes fails the wait at the timeout.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
SCRIPT=$ROOT/.github/scripts/ci-background.sh
TMP=${TMPDIR:-/tmp}/colliedesk-ci-background-test-$$
mkdir -p "$TMP"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

fail() { echo "FAIL: $*" >&2; exit 1; }
bg() { env CI_BACKGROUND_DIR="$TMP" CI_BACKGROUND_INTERVAL=0.1 "$@"; }

started=$(date +%s)
bg sh "$SCRIPT" start slow sh -c 'sleep 2; echo slow done' || fail "start failed"
[ $(($(date +%s) - started)) -lt 2 ] || fail "start waited for the command"
bg sh "$SCRIPT" wait slow >"$TMP/out" || fail "wait failed for a command that passed"
grep -q "slow done" "$TMP/out" || fail "wait did not print the command output"

bg sh "$SCRIPT" start broken sh -c 'echo broken output; exit 3'
if bg sh "$SCRIPT" wait broken >"$TMP/out" 2>&1; then fail "wait passed for a command that failed"; fi
grep -q "broken output" "$TMP/out" || fail "a failed command's output is not shown"

bg sh "$SCRIPT" start stuck sleep 30
if bg CI_BACKGROUND_TIMEOUT=1 sh "$SCRIPT" wait stuck >/dev/null 2>"$TMP/err"; then fail "wait passed a command that never finished"; fi
grep -q "stuck" "$TMP/err" || fail "the timeout does not name the command"
if bg sh "$SCRIPT" wait never-started >/dev/null 2>&1; then fail "wait passed a command that was never started"; fi
echo "ci-background passes on output and exit status"
