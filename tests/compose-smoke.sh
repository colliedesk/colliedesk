#!/bin/sh
# Builds the repository's own compose.yaml ("From a clone" in docs/self-host/install.md), starts it and
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
cleanup() { compose down -v --remove-orphans >/dev/null 2>&1 || :; }
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'compose smoke: %s\n' "$1" >&2
  compose logs --no-color >&2 || :
  exit 1
}

compose up -d --build >/dev/null 2>&1 || fail 'docker compose up failed'
i=0
until curl -fsS "http://127.0.0.1:$COLLIE_DESK_PORT/" >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -le 60 ] || fail "server did not answer on port $COLLIE_DESK_PORT within 60 s"
  sleep 1
done
printf '%s\n' 'compose smoke passed'
