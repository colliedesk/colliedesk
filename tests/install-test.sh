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
  # Real `docker compose exec` forwards stdin even with -T; consume it the same way.
  cat >/dev/null || :
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
# Fresh installs: superuser only for initdb, a non-superuser owner, and a separate request login.
grep -Fx 'POSTGRES_USER=postgres' "$fresh/.env" >/dev/null
grep -E '^DATABASE_URL=postgres://colliedesk:[0-9a-f]{64}@postgres:5432/colliedesk\?sslmode=disable$' "$fresh/.env" >/dev/null
grep -E '^DATABASE_REQUEST_URL=postgres://colliedesk_request:[0-9a-f]{64}@postgres:5432/colliedesk\?sslmode=disable$' "$fresh/.env" >/dev/null
grep -E '^COLLIEDESK_OWNER_PASSWORD=[0-9a-f]{64}$' "$fresh/.env" >/dev/null
[ -f "$fresh/initdb/10-colliedesk-owner.sh" ]
# The owner password reaches psql through its environment, never its argv (visible in ps).
grep -F '\getenv owner_password COLLIEDESK_OWNER_PASSWORD' "$fresh/initdb/10-colliedesk-owner.sh" >/dev/null
if grep -F 'COLLIEDESK_OWNER_PASSWORD"' "$fresh/initdb/10-colliedesk-owner.sh" >/dev/null; then
  printf '%s\n' 'initdb script passes the owner password on the psql command line' >&2
  exit 1
fi
grep -F './initdb:/docker-entrypoint-initdb.d:ro' "$fresh/compose.yaml" >/dev/null
grep -F 'DATABASE_REQUEST_URL: ${DATABASE_REQUEST_URL:-}' "$fresh/compose.yaml" >/dev/null
# The usage report opt-out set in .env (or the shell) must reach the server container.
grep -F 'DISABLE_TELEMETRY: ${DISABLE_TELEMETRY:-}' "$fresh/compose.yaml" >/dev/null
grep -F 'DO_NOT_TRACK: ${DO_NOT_TRACK:-}' "$fresh/compose.yaml" >/dev/null
# Installs whose .env predates the request login keep connecting as before.
grep -F 'DATABASE_URL: ${DATABASE_URL:-postgres://colliedesk:${POSTGRES_PASSWORD}@postgres:5432/colliedesk?sslmode=disable}' "$existing/compose.yaml" >/dev/null
grep -F 'POSTGRES_USER: ${POSTGRES_USER:-colliedesk}' "$existing/compose.yaml" >/dev/null
printf '%s' "$output" | grep -F 'http://localhost:8080/redeem?token=pairing-token' >/dev/null
if printf '%s' "$output" | grep -F 0123456789abcdef >/dev/null; then
  printf '%s\n' 'installer printed a generated secret' >&2
  exit 1
fi

piped=$TMP/piped
# Feed the script line by line, like `curl | sh` over a network, so sh cannot buffer all of it.
output=$(while IFS= read -r line || [ -n "$line" ]; do printf '%s\n' "$line"; sleep 0.01; done <"$ROOT/install.sh" | sh -s -- --dir "$piped" --name Desk --email admin@example.com 2>&1)
printf '%s' "$output" | grep -F 'http://localhost:8080/redeem?token=pairing-token' >/dev/null || {
  printf 'piped install did not print the pairing URL:\n%s\n' "$output" >&2
  exit 1
}

printf '%s\n' 'install tests passed'
