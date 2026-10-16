#!/bin/sh
# Decisions for the manual Release button (release.yml, workflow_dispatch).
#   cloud-commit <version>  version from GET /api/v1/config -> the git ref to release:
#                           edge-<hex> (a main build) -> <hex>; v0.0.N (a release) -> the tag.
#   next-tag [tag...]       the alpha tag after the highest v0.0.N given; v0.0.1 when none.
set -eu

case ${1-} in
  cloud-commit)
    version=${2-}
    if printf '%s' "$version" | grep -Eq '^edge-[0-9a-f]{7,40}$'; then
      printf '%s\n' "${version#edge-}"
    elif printf '%s' "$version" | grep -Eq '^v0\.0\.(0|[1-9][0-9]*)$'; then
      printf '%s\n' "$version"
    else
      echo "Cloud reports version '$version', which is not a main build (edge-<commit>) or a release." >&2
      exit 1
    fi
    ;;
  next-tag)
    shift
    highest=$(printf '%s\n' "$@" | grep -E '^v0\.0\.(0|[1-9][0-9]*)$' | sed 's/^v0\.0\.//' | sort -n | tail -n 1)
    printf 'v0.0.%s\n' "$(( ${highest:-0} + 1 ))"
    ;;
  *)
    echo "usage: release-next.sh cloud-commit <version> | next-tag [tag...]" >&2
    exit 2
    ;;
esac
