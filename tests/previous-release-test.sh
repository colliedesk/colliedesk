#!/bin/sh
# Which release the upgrade test starts from (.github/scripts/previous-release.sh).
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
failed=0

expect() { # expect <want> <tags, one per line> [candidate tag]
  want=$1 tags=$2
  shift 2
  got=$(PREVIOUS_RELEASE_TAGS=$tags sh "$ROOT/.github/scripts/previous-release.sh" "$@") || got='<failed>'
  [ "$got" = "$want" ] || { echo "previous-release.sh $* with tags '$tags': got '$got', want '$want'" >&2; failed=1; }
}

image=ghcr.io/colliedesk/colliedesk
all=$(printf 'v0.0.1\nv0.0.2\nv0.0.10\nv0.1.0-rc.1\nandroid-v9.9.9\n')
expect "$image:0.0.10" "$all"
expect "$image:0.0.10" "$all" v0.0.11
# A run finishing a release whose tag already exists starts from the release before it.
expect "$image:0.0.2" "$all" v0.0.10
# A patch for an older line starts from that line, not from the newest release.
expect "$image:0.0.1" "$all" v0.0.2
expect '' "$all" v0.0.1
expect '' ''
expect '' 'v0.1.0-rc.1'

[ "$failed" = 0 ] && echo 'previous-release tests passed'
exit "$failed"
