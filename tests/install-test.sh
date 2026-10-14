#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
TMP=${TMPDIR:-/tmp}/colliedesk-install-test-$$
BIN=$TMP/bin
mkdir -p "$BIN"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

cat >"$BIN/docker" <<'STUB'
#!/bin/sh
set -eu
if [ "${1:-}" = compose ] && [ "${2:-}" = version ]; then
  [ "${STUB_MODE:-ok}" != no-compose ]
  exit
fi
if [ "${1:-}" = compose ] && [ "${2:-}" = ps ]; then
  if [ "${3:-}" = --quiet ] && [ "${4:-}" = server ]; then
    printf '%s\n' test-server-id
  fi
  exit
fi
if [ "${1:-}" = inspect ]; then
  printf '%s\n' healthy
  exit
fi
if [ "${1:-}" = compose ] && [ "${2:-}" = exec ]; then
  printf '%s\n' 'http://localhost:8080/redeem?token=pairing-token'
  exit
fi
exit 0
STUB

cat >"$BIN/openssl" <<'STUB'
#!/bin/sh
printf '%s\n' 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
STUB

chmod +x "$BIN/docker" "$BIN/openssl"
PATH=$BIN:$PATH
export PATH
SLEEP=:
export SLEEP

expect_failure() {
  expected=$1
  shift
  if output=$({ "$@" </dev/null; } 2>&1); then
    printf 'expected failure: %s\n' "$expected" >&2
    exit 1
  fi
  printf '%s' "$output" | grep -F "$expected" >/dev/null
}

expect_failure 'Docker is required' env DOCKER=missing-docker sh "$ROOT/install.sh" --name Desk --email admin@example.com
expect_failure 'Docker Compose v2 is required' env STUB_MODE=no-compose sh "$ROOT/install.sh" --name Desk --email admin@example.com
expect_failure 'Non-interactive install requires --name and --email' sh "$ROOT/install.sh"

existing=$TMP/existing
mkdir -p "$existing"
printf '%s\n' 'KEEP=unchanged' >"$existing/.env"
output=$(sh "$ROOT/install.sh" --dir "$existing" --name Desk --email admin@example.com </dev/null)
[ "$(cat "$existing/.env")" = 'KEEP=unchanged' ]
printf '%s' "$output" | grep -F 'http://localhost:8080/redeem?token=pairing-token' >/dev/null

fresh=$TMP/fresh
output=$(sh "$ROOT/install.sh" --dir "$fresh" --name Desk --email admin@example.com </dev/null)
[ "$(stat -c %a "$fresh/.env")" = 600 ]
grep -F "image: \${COLLIE_DESK_IMAGE}" "$fresh/compose.yaml" >/dev/null
printf '%s' "$output" | grep -F 'http://localhost:8080/redeem?token=pairing-token' >/dev/null
if printf '%s' "$output" | grep -F 0123456789abcdef >/dev/null; then
  printf '%s\n' 'installer printed a generated secret' >&2
  exit 1
fi

printf '%s\n' 'install tests passed'
