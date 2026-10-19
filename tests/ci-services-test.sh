#!/bin/sh
# Checks .github/scripts/ci-services.sh against a stubbed docker: wait returns once every service
# answers, fails naming the service that never did, and start refuses unknown services.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
SCRIPT=$ROOT/.github/scripts/ci-services.sh
TMP=${TMPDIR:-/tmp}/colliedesk-ci-services-test-$$
mkdir -p "$TMP"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

# The stub's readiness probe succeeds from call READY_AFTER on (0 = never); docker run exits RUN_EXIT.
cat >"$TMP/docker" <<'STUB'
#!/bin/sh
case "$1" in
  run) exit "${RUN_EXIT:-0}" ;;
  exec)
    calls=$(($(cat "$STUB_DIR/calls" 2>/dev/null || echo 0) + 1))
    echo "$calls" >"$STUB_DIR/calls"
    [ "$READY_AFTER" -gt 0 ] && [ "$calls" -ge "$READY_AFTER" ]
    ;;
  *) exit 0 ;;
esac
STUB
chmod +x "$TMP/docker"

run() {
  rm -f "$TMP/calls"
  env STUB_DIR="$TMP" CI_SERVICES_DIR="${CI_SERVICES_DIR:-$TMP/state-default}" DOCKER="$TMP/docker" CI_SERVICES_TIMEOUT=3 CI_SERVICES_INTERVAL=0.1 "$@"
}

fail() { echo "FAIL: $*" >&2; exit 1; }

run READY_AFTER=3 sh "$SCRIPT" wait postgres >/dev/null || fail "wait gave up on a service that became ready"
if run READY_AFTER=0 sh "$SCRIPT" wait postgres >/dev/null 2>"$TMP/err"; then
  fail "wait passed although postgres never answered"
fi
grep -q postgres "$TMP/err" || fail "the timeout error does not name postgres: $(cat "$TMP/err")"
if run READY_AFTER=1 sh "$SCRIPT" start redis >/dev/null 2>&1; then fail "start accepted an unknown service"; fi
# start returns at once (the pull runs in the background); a failed docker run fails the wait, fast.
run READY_AFTER=0 RUN_EXIT=1 CI_SERVICES_DIR="$TMP/state" sh "$SCRIPT" start postgres >/dev/null 2>&1 ||
  fail "start did not return while docker run ran in the background"
sleep 0.3
if run READY_AFTER=0 CI_SERVICES_TIMEOUT=30 CI_SERVICES_DIR="$TMP/state" sh "$SCRIPT" wait postgres >/dev/null 2>"$TMP/err"; then
  fail "wait passed although docker run failed"
fi
grep -q "could not start" "$TMP/err" || fail "a failed docker run is not reported: $(cat "$TMP/err")"
run READY_AFTER=1 CI_SERVICES_DIR="$TMP/ok" sh "$SCRIPT" start postgres mailpit >/dev/null || fail "start failed for known services"
echo "ci-services waits for readiness and fails closed"
