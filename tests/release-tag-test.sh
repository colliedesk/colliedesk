#!/bin/sh
# Releases follow Semantic Versioning from 1.0.0: release.yml accepts vMAJOR.MINOR.PATCH with
# MAJOR >= 1, and the alpha releases already made (v0.0.N). Every other 0.x tag and every
# pre-release suffix is refused; changing that is a deliberate change to
# .github/scripts/release-tag-check.sh.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
CHECK="$ROOT/.github/scripts/release-tag-check.sh"
failed=0

for tag in v0.0.1 v0.0.9 v0.0.23 v1.0.0 v1.0.1 v1.2.0 v2.0.0 v10.20.30; do
  sh "$CHECK" "$tag" >/dev/null 2>&1 || { echo "expected $tag to be accepted" >&2; failed=1; }
done
for tag in v0.1.0 v0.2.3 v0.0.1-beta.1 v0.0.1-alpha.1 v1.0.0-rc.1 v1.0.0-beta v1.0 v01.0.0 \
  v1.01.0 v1.0.01 1.0.0 v0.0 0.0.1 v0.0.01 edge ''; do
  if sh "$CHECK" "$tag" >/dev/null 2>&1; then echo "expected '$tag' to be refused" >&2; failed=1; fi
done

[ "$failed" = 0 ] && echo "release tag gate ok"
exit "$failed"
