#!/bin/sh
# Builds the repository's own compose.yaml ("From a clone" in docs/self-host/README.md), starts it and
# proves the server answers. Needs Docker with Compose v2 and curl:
#   sh tests/compose-smoke.sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
# Ports derive from the checkout path, so two checkouts on one host do not collide.
OFFSET=$(($(printf '%s' "$ROOT" | cksum | cut -d" " -f1) % 800))
export COLLIE_DESK_PORT=${COLLIE_DESK_PORT:-$((19100 + OFFSET))}
export POSTGRES_HOST_PORT=${POSTGRES_HOST_PORT:-$((20100 + OFFSET))}
PROJECT=colliedesk-compose-smoke-$$

compose() { docker compose -p "$PROJECT" -f "$ROOT/compose.yaml" "$@"; }
UP_LOG=$(mktemp)
cleanup() { compose down -v --remove-orphans >/dev/null 2>&1 || :; rm -f "$UP_LOG"; }
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'compose smoke: %s\n' "$1" >&2
  compose logs --no-color >&2 || :
  exit 1
}

# Quiet on success; on failure the build or start output says why (containers may not exist yet).
if ! compose up -d --build >"$UP_LOG" 2>&1; then
  cat "$UP_LOG" >&2
  fail 'docker compose up failed'
fi
i=0
until curl -fsS "http://127.0.0.1:$COLLIE_DESK_PORT/" >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -le 60 ] || fail "server did not answer on port $COLLIE_DESK_PORT within 60 s"
  sleep 1
done
printf '%s\n' 'compose smoke passed'
