#!/bin/sh
# Decisions behind the manual Release button: which commit Cloud runs, the last release, and the
# next Semantic Versioning tag for the part the person chose (major, minor or patch).
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
expect v1.0.0 cloud-commit v1.0.0
expect v1.12.3 cloud-commit v1.12.3
expect '<refused>' cloud-commit v0.1.0
expect '<refused>' cloud-commit v1.0.0-rc.1
expect '<refused>' cloud-commit dev
expect '<refused>' cloud-commit ''
expect '<refused>' cloud-commit edge-
expect '<refused>' cloud-commit 'edge-f7f0;rm'
expect '<refused>' cloud-commit sha-f7f0e0a72c9c
# A second line could smuggle a workflow command (::stop-commands::) into the log.
expect '<refused>' cloud-commit "$(printf 'edge-f7f0e0a72c9c\n::stop-commands::x')"
expect '<refused>' cloud-commit "$(printf 'v0.0.1\nv0.0.2')"

# The highest release, by version and not by text; other tags never count.
expect '' last-tag
expect v0.0.1 last-tag v0.0.1 android-beta
expect v1.10.0 last-tag v0.0.1 v1.9.0 v1.10.0 v1.2.0 v2.0.0-rc.1 v0.1.0

# The first release after the alpha ones is 1.0.0, whichever part was chosen.
expect v1.0.0 next-tag minor
expect v1.0.0 next-tag patch v0.0.1
expect v1.0.0 next-tag minor v0.0.1
expect v1.0.0 next-tag major v0.0.1 v0.0.2
# From then on, Semantic Versioning.
expect v1.1.0 next-tag minor v0.0.1 v1.0.0
expect v1.1.1 next-tag patch v1.0.0 v1.1.0 v0.0.1
expect v2.0.0 next-tag major v1.4.2
expect v1.11.0 next-tag minor v1.9.0 v1.10.0 v1.2.0
expect v1.2.4 next-tag patch v1.2.3 android-beta v1.3.0-rc.1 v0.1.0 v9.0
expect '<refused>' next-tag
expect '<refused>' next-tag huge v1.0.0
expect '<refused>' next-tag v1.0.0

[ "$failed" = 0 ] && echo "release next ok"
exit "$failed"
