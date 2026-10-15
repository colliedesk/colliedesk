#!/bin/sh
# Fails when a workflow uses an action by tag or branch instead of a full commit SHA, or when the
# Dockerfile pulls a base image without a digest. A moved tag must never change what CI runs.
# Dependabot keeps the pins current. Usage: tests/pinned-dependencies-test.sh [repo root]
set -eu

ROOT=${1:-$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)}

unpinned_actions=$(grep -nE '^[[:space:]-]*uses:[[:space:]]' "$ROOT"/.github/workflows/*.yml \
  | grep -vE 'uses:[[:space:]]+\./' \
  | grep -vE 'uses:[[:space:]]+[^[:space:]@]+@[0-9a-f]{40}([[:space:]]|$)' || true)

stages=$(awk 'toupper($1) == "FROM" { for (i = 2; i <= NF; i++) if (toupper($i) == "AS") printf " %s", $(i + 1) }' \
  "$ROOT/server/Dockerfile")
unpinned_images=$(awk -v stages="$stages " '
  toupper($1) == "FROM" {
    image = $2; if (image ~ /^--/) image = $3
    if (index(stages, " " image " ") || image ~ /@sha256:[0-9a-f]{64}$/) next
    printf "server/Dockerfile:%d: %s\n", NR, $0
  }' "$ROOT/server/Dockerfile")

if [ -n "$unpinned_actions$unpinned_images" ]; then
  echo "Pin these to a full commit SHA or image digest (add the version as a comment):" >&2
  [ -z "$unpinned_actions" ] || echo "$unpinned_actions" >&2
  [ -z "$unpinned_images" ] || echo "$unpinned_images" >&2
  exit 1
fi
echo "every action and base image is pinned"
