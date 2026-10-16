#!/bin/sh
# Decisions behind the manual Release button: which commit Cloud runs, and the next alpha tag.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
NEXT="$ROOT/.github/scripts/release-next.sh"
failed=0

expect() { # expect <want> <command...>
  want=$1; shift
  got=$(sh "$NEXT" "$@" 2>/dev/null) || got="<refused>"
  [ "$got" = "$want" ] || { echo "release-next.sh $*: got '$got', want '$want'" >&2; failed=1; }
}

expect f7f0e0a72c9c cloud-commit edge-f7f0e0a72c9c
expect v0.0.1 cloud-commit v0.0.1
expect '<refused>' cloud-commit dev
expect '<refused>' cloud-commit ''
expect '<refused>' cloud-commit edge-
expect '<refused>' cloud-commit 'edge-f7f0;rm'
expect '<refused>' cloud-commit sha-f7f0e0a72c9c

expect v0.0.1 next-tag
expect v0.0.2 next-tag v0.0.1
expect v0.0.10 next-tag v0.0.2 v0.0.9 v0.0.1
expect v0.0.4 next-tag v0.0.3 android-beta v0.0.1-alpha.1 v1.2.3

[ "$failed" = 0 ] && echo "release next ok"
exit "$failed"
