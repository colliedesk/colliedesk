#!/bin/sh
# Real installer smoke test: runs install.sh with Docker against a locally built image, then
# proves the printed pairing link signs in. Needs Docker with Compose v2, curl and a built image:
#   docker build -t colliedesk:smoke -f server/Dockerfile .
#   COLLIE_DESK_IMAGE=colliedesk:smoke sh tests/install-smoke.sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
: "${COLLIE_DESK_IMAGE:?set COLLIE_DESK_IMAGE to a locally built image}"
# Default port derives from the checkout path, so two checkouts on one host do not collide.
PORT=${COLLIE_DESK_PORT:-$((18100 + $(printf '%s' "$ROOT" | cksum | cut -d" " -f1) % 800))}
TMP=${TMPDIR:-/tmp}/colliedesk-install-smoke-$$
# The installer's compose file names the project "colliedesk"; a unique name keeps this test away
# from any real installation on the same Docker host.
COMPOSE_PROJECT_NAME=colliedesk-install-smoke-$$
export COLLIE_DESK_IMAGE COMPOSE_PROJECT_NAME
export COLLIE_DESK_PORT="$PORT"
# A smoke install is not a real one: keep it out of the usage report counts.
export DISABLE_TELEMETRY=true

cleanup() {
  if [ -f "$TMP/compose.yaml" ]; then
    (cd "$TMP" && docker compose down -v --remove-orphans >/dev/null 2>&1) || :
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'install smoke: %s\n' "$1" >&2
  if [ -f "$TMP/compose.yaml" ]; then (cd "$TMP" && docker compose logs --no-color server >&2) || :; fi
  exit 1
}

# The release image must not contain the end-to-end Freshdesk network seam (build tag e2e).
seams=$(docker run --rm --entrypoint grep "$COLLIE_DESK_IMAGE" -c -e COLLIEDESK_E2E -e UseE2ENetwork /app/collie || :)
[ "$seams" = 0 ] || fail "image $COLLIE_DESK_IMAGE contains the e2e test seam ($seams matches); build without -tags e2e"

output=$(sh "$ROOT/install.sh" --dir "$TMP" --name "Smoke Desk" --email Admin@Smoke.example </dev/null 2>&1) || fail "installer failed: $output"
printf '%s\n' "$output"
url=$(printf '%s\n' "$output" | grep -E "^http://localhost:$PORT/redeem\?token=[A-Za-z0-9_-]+$" | tail -n 1)
[ -n "$url" ] || fail "installer printed no pairing URL for port $PORT"
printf '%s' "$output" | grep -F 'collie pair --workspace smoke-desk --email admin@smoke.example' >/dev/null ||
  fail 'installer output does not say how to print a new link'
# Every generated secret: *_PASSWORD, APP_SECRET, and the passwords inside the DATABASE*_URL values.
secrets=$( { sed -n 's/^[A-Z_]*_PASSWORD=//p; s/^APP_SECRET=//p' "$TMP/.env"
  sed -n 's/^DATABASE[A-Z_]*_URL=postgres:\/\/[^:]*:\([^@]*\)@.*/\1/p' "$TMP/.env"; } | sort -u)
[ "$(printf '%s\n' "$secrets" | grep -c .)" = 4 ] || fail "expected 4 distinct generated secrets in .env, found: $(printf '%s\n' "$secrets" | grep -c .)"
for secret in $secrets; do
  case $output in *"$secret"*) fail 'installer printed a generated secret' ;; esac
done

# The link opens the web app...
page=$(curl -fsS "$url") || fail "pairing URL did not load"
case $page in *'<div id="root">'*) ;; *) fail 'pairing URL did not serve the web app' ;; esac

# ...and redeeming it signs the admin in.
token=${url#*token=}
session=$(curl -fsS -X POST "http://localhost:$PORT/api/v1/auth/redeem" -H 'Content-Type: application/json' \
  -d "{\"token\":\"$token\",\"session_type\":\"bearer\",\"label\":\"install smoke\"}") || fail 'pairing link did not redeem'
bearer=$(printf '%s' "$session" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
[ -n "$bearer" ] || fail "redeem returned no session: $session"
me=$(curl -fsS "http://localhost:$PORT/api/v1/auth/me" -H "Authorization: Bearer $bearer") || fail 'session was refused'
printf '%s' "$me" | grep -F '"email":"admin@smoke.example"' >/dev/null || fail "signed in as the wrong agent: $me"
printf '%s' "$me" | grep -F '"name":"Smoke Desk"' >/dev/null || fail "signed in to the wrong workspace: $me"
printf '%s' "$me" | grep -F '"role":"admin"' >/dev/null || fail "first agent is not an admin: $me"

# The link works once.
status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://localhost:$PORT/api/v1/auth/redeem" \
  -H 'Content-Type: application/json' -d "{\"token\":\"$token\"}")
[ "$status" = 401 ] || fail "a used pairing link answered $status, want 401"

# Re-running the installer keeps the workspace and .env.
before=$(sha256sum "$TMP/.env")
again=$(sh "$ROOT/install.sh" --dir "$TMP" --name "Smoke Desk" --email admin@smoke.example </dev/null 2>&1) || fail "re-run failed: $again"
printf '%s' "$again" | grep -F 'Existing workspace initialization was kept.' >/dev/null || fail "re-run did not keep the workspace: $again"
[ "$(sha256sum "$TMP/.env")" = "$before" ] || fail 're-run changed .env'
curl -fsS "http://localhost:$PORT/api/v1/auth/me" -H "Authorization: Bearer $bearer" >/dev/null || fail 'session lost after re-run'

printf '%s\n' 'install smoke passed'
