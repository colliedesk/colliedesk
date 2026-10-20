#!/bin/sh
# Prints the image of the release an upgrade test starts from: the highest stable vX.Y.Z tag below
# CANDIDATE_TAG (or the highest one, without it). Reads tags from the remote, since CI checkouts
# have none. Prints nothing when there is no earlier release.
#   sh .github/scripts/previous-release.sh [CANDIDATE_TAG]
# PREVIOUS_RELEASE_TAGS (one tag per line) replaces the remote, for tests.
set -eu

CANDIDATE=${1:-}
REMOTE=${PREVIOUS_RELEASE_REMOTE:-origin}
if [ -n "${PREVIOUS_RELEASE_TAGS+set}" ]; then
  tags=$PREVIOUS_RELEASE_TAGS
else
  # Its own command, so a remote that cannot be read fails here instead of reading as "no release".
  refs=$(git ls-remote --tags --refs "$REMOTE" 'v*') || {
    echo "previous-release: cannot list the tags of $REMOTE" >&2
    exit 1
  }
  tags=$(printf '%s\n' "$refs" | sed 's#.*refs/tags/##')
fi

# Prereleases (v1.2.0-rc.1) are never the starting point: self-hosters run stable releases.
previous=$(printf '%s\n' "$tags" | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | {
  if [ -n "$CANDIDATE" ]; then
    # Every tag sorted with the candidate; the previous release is the one just before it.
    { cat; echo "$CANDIDATE"; } | sort -u -V | awk -v c="$CANDIDATE" '$0 == c { print last; exit } { last = $0 }'
  else
    sort -V | tail -n 1
  fi
})
[ -z "$previous" ] || echo "ghcr.io/colliedesk/colliedesk:${previous#v}"
