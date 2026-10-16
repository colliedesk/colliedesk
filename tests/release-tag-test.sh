#!/bin/sh
# Colliedesk stays alpha until HC Labs announces beta: release.yml accepts only v0.0.N tags.
# Moving to beta (0.1.0) is a deliberate change to .github/scripts/release-tag-check.sh.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
CHECK="$ROOT/.github/scripts/release-tag-check.sh"
failed=0

for tag in v0.0.1 v0.0.9 v0.0.23; do
  sh "$CHECK" "$tag" >/dev/null 2>&1 || { echo "expected $tag to be accepted" >&2; failed=1; }
done
for tag in v0.1.0 v0.0.1-beta.1 v0.0.1-alpha.1 v1.0.0 v0.0 0.0.1 v0.0.01 edge ''; do
  if sh "$CHECK" "$tag" >/dev/null 2>&1; then echo "expected '$tag' to be refused" >&2; failed=1; fi
done

[ "$failed" = 0 ] && echo "release tag gate ok"
exit "$failed"
