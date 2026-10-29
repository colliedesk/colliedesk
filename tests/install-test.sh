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
if [ "${1:-}" = run ]; then
  # The installer asks the image whether it stores settings (collie config).
  [ "${STUB_MODE:-ok}" = old-image ] || printf '%s\n' 'Usage: collie <serve|migrate|config|create-workspace|pair>'
  exit
fi
if [ "${1:-}" = compose ] && [ "${2:-}" = run ]; then
  # The import of settings left in .env: record what the installer sends on stdin.
  printf '%s\n' "$*" >>"$STUB_LOG"
  cat >>"$STUB_LOG.stdin"
  exit
fi
if [ "${1:-}" = compose ] && [ "${2:-}" = exec ]; then
  # Real `docker compose exec` forwards stdin even with -T; consume it the same way.
  cat >/dev/null || :
  printf '%s\n' 'http://localhost:8080/sign-in?token=pairing-token'
  exit
fi
exit 0
STUB

cat >"$BIN/openssl" <<'STUB'
#!/bin/sh
if [ "$2" = -base64 ]; then
  printf '%s\n' MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=
  exit
fi
printf '%s\n' 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
STUB

chmod +x "$BIN/docker" "$BIN/openssl"
PATH=$BIN:$PATH
export PATH
SLEEP=:
STUB_LOG=$TMP/docker.log
export SLEEP STUB_LOG

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
printf '%s' "$output" | grep -F 'http://localhost:8080/sign-in?token=pairing-token' >/dev/null

fresh=$TMP/fresh
rm -f "$STUB_LOG" "$STUB_LOG.stdin"
output=$(sh "$ROOT/install.sh" --dir "$fresh" --name Desk --email admin@example.com </dev/null)
[ "$(stat -c %a "$fresh/.env")" = 600 ]
grep -F "image: \${COLLIE_DESK_IMAGE}" "$fresh/compose.yaml" >/dev/null
# Fresh installs: superuser only for initdb, a non-superuser owner, and a separate request login.
grep -Fx 'POSTGRES_USER=postgres' "$fresh/.env" >/dev/null
grep -E '^DATABASE_URL=postgres://colliedesk:[0-9a-f]{64}@postgres:5432/colliedesk\?sslmode=disable$' "$fresh/.env" >/dev/null
grep -E '^DATABASE_REQUEST_URL=postgres://colliedesk_request:[0-9a-f]{64}@postgres:5432/colliedesk\?sslmode=disable$' "$fresh/.env" >/dev/null
grep -E '^COLLIEDESK_OWNER_PASSWORD=[0-9a-f]{64}$' "$fresh/.env" >/dev/null
# A fresh install gets a key ring, so Gmail grants and in-app import keys are sealed and survive restarts.
grep -Ex 'MAILBOX_TOKEN_KEYS=k1:[A-Za-z0-9+/]{43}=' "$fresh/.env" >/dev/null
# The server never read APP_SECRET: a fresh install neither generates it nor passes it to the server.
if grep -F APP_SECRET "$fresh/.env" "$fresh/compose.yaml" >/dev/null; then
  printf '%s\n' 'installer still writes APP_SECRET, which the server does not read' >&2
  exit 1
fi
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
# Settings live in the database (collie config); the server's compose passes only what opens it.
for moved in SMTP_HOST EMAIL_FROM_POLICY MAIL_FROM_DOMAIN GOOGLE_OAUTH_CLIENT_ID MICROSOFT_OAUTH_TENANT TRUSTED_PROXIES; do
  if grep -F "$moved" "$fresh/compose.yaml" >/dev/null; then
    printf 'compose.yaml still passes %s to the server\n' "$moved" >&2
    exit 1
  fi
done
grep -F 'MAILBOX_TOKEN_KEYS: ${MAILBOX_TOKEN_KEYS:-}' "$fresh/compose.yaml" >/dev/null
# An install without a key ring keeps reading its secrets from .env, so the compose file still passes them.
grep -F 'SMTP_PASSWORD: ${SMTP_PASSWORD:-}' "$fresh/compose.yaml" >/dev/null
# A fresh install saves the reverse proxy on this host as trusted, as its compose file did before; nothing else.
[ "$(cat "$STUB_LOG.stdin")" = 'TRUSTED_PROXIES=127.0.0.0/8,::1/128,172.16.0.0/12' ]
# Installs whose .env predates the request login keep connecting as before.
grep -F 'DATABASE_URL: ${DATABASE_URL:-postgres://colliedesk:${POSTGRES_PASSWORD}@postgres:5432/colliedesk?sslmode=disable}' "$existing/compose.yaml" >/dev/null
grep -F 'POSTGRES_USER: ${POSTGRES_USER:-colliedesk}' "$existing/compose.yaml" >/dev/null
printf '%s' "$output" | grep -F 'http://localhost:8080/sign-in?token=pairing-token' >/dev/null
if printf '%s' "$output" | grep -F 0123456789abcdef >/dev/null; then
  printf '%s\n' 'installer printed a generated secret' >&2
  exit 1
fi

piped=$TMP/piped
# Feed the script line by line, like `curl | sh` over a network, so sh cannot buffer all of it.
output=$(while IFS= read -r line || [ -n "$line" ]; do printf '%s\n' "$line"; sleep 0.01; done <"$ROOT/install.sh" | sh -s -- --dir "$piped" --name Desk --email admin@example.com 2>&1)
printf '%s' "$output" | grep -F 'http://localhost:8080/sign-in?token=pairing-token' >/dev/null || {
  printf 'piped install did not print the pairing URL:\n%s\n' "$output" >&2
  exit 1
}

# Running the installer again on an older install keeps its compose file as compose.yaml.bak (to go back to
# the older image) and moves the settings still in .env into the database, once, before the server starts.
upgrade=$TMP/upgrade
mkdir -p "$upgrade"
printf '%s\n' 'name: colliedesk' '# the older compose file' >"$upgrade/compose.yaml"
printf '%s\n' 'POSTGRES_PASSWORD=pg' 'APP_ORIGIN=https://desk.example.com' 'SMTP_HOST=smtp.example.net' 'SMTP_PASSWORD=relay-pass' \
  'INTERNAL_SIGNUP_EMAILS=@example.com' 'SMTP_PORT=' 'APP_SECRET=old' >"$upgrade/.env"
touch "$upgrade/.initialized"
rm -f "$STUB_LOG" "$STUB_LOG.stdin"
sh "$ROOT/install.sh" --dir "$upgrade" --name Desk --email admin@example.com </dev/null >/dev/null
grep -Fx '# the older compose file' "$upgrade/compose.yaml.bak" >/dev/null
grep -F 'compose run --rm -T server' "$STUB_LOG" >/dev/null
grep -F 'collie config import --stdin --only-missing' "$STUB_LOG" >/dev/null
grep -Fx 'SMTP_HOST=smtp.example.net' "$STUB_LOG.stdin" >/dev/null
grep -Fx 'SMTP_PASSWORD=relay-pass' "$STUB_LOG.stdin" >/dev/null
grep -Fx 'INTERNAL_SIGNUP_EMAILS=@example.com' "$STUB_LOG.stdin" >/dev/null
# Only settings: never the database password, never an empty value.
if grep -E '^(POSTGRES_PASSWORD|APP_ORIGIN|APP_SECRET|SMTP_PORT)=' "$STUB_LOG.stdin" >/dev/null; then
  printf 'the installer imported a line that is not a setting:\n%s\n' "$(cat "$STUB_LOG.stdin")" >&2
  exit 1
fi

# .env that sets TRUSTED_PROXIES empty trusts no proxy, which the server does with nothing saved.
grep -Fx 'TRUSTED_PROXIES=127.0.0.0/8,::1/128,172.16.0.0/12' "$STUB_LOG.stdin" >/dev/null
noproxy=$TMP/noproxy
mkdir -p "$noproxy"
printf '%s\n' 'POSTGRES_PASSWORD=pg' 'TRUSTED_PROXIES=' >"$noproxy/.env"
rm -f "$STUB_LOG" "$STUB_LOG.stdin"
sh "$ROOT/install.sh" --dir "$noproxy" --name Desk --email admin@example.com </dev/null >/dev/null
[ ! -f "$STUB_LOG.stdin" ]

# An image without collie config stops the installer before it changes the compose file.
old=$TMP/old
mkdir -p "$old"
printf '%s\n' 'name: colliedesk' '# kept' >"$old/compose.yaml"
printf '%s\n' 'POSTGRES_PASSWORD=pg' 'SMTP_HOST=smtp.example.net' >"$old/.env"
expect_failure 'is older than this installer' env STUB_MODE=old-image sh "$ROOT/install.sh" --dir "$old" --name Desk --email admin@example.com
grep -Fx '# kept' "$old/compose.yaml" >/dev/null
[ ! -f "$old/compose.yaml.bak" ]

printf '%s\n' 'install tests passed'
