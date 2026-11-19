#!/bin/sh
# Checks tests/compose-smoke.sh: when `docker compose up` fails, the smoke prints why, so a CI
# failure can be read without a rerun. A stand-in docker on PATH fails the way a real build does.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
TMP=${TMPDIR:-/tmp}/colliedesk-compose-up-output-test-$$
mkdir -p "$TMP/bin"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

fail() { echo "FAIL: $*" >&2; exit 1; }

cat >"$TMP/bin/docker" <<'EOF'
#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = up ]; then
    echo 'failed to solve: process "/bin/sh -c pnpm build" did not complete successfully' >&2
    exit 1
  fi
done
exit 0
EOF
chmod +x "$TMP/bin/docker"

if PATH="$TMP/bin:$PATH" sh "$ROOT/tests/compose-smoke.sh" >"$TMP/out" 2>&1; then
  fail "compose smoke passed although docker compose up failed"
fi
grep -q 'docker compose up failed' "$TMP/out" || fail "the failure is not named"
grep -q 'pnpm build" did not complete' "$TMP/out" || fail "docker compose up output is hidden"
echo "compose smoke shows why docker compose up failed"
