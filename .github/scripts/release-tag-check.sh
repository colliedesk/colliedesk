#!/bin/sh
# Accepts only the release tags allowed today. Colliedesk follows Semantic Versioning from 1.0.0:
# vMAJOR.MINOR.PATCH with MAJOR >= 1, plus the alpha releases made before it (v0.0.N). Any other
# 0.x tag and any pre-release suffix is refused. Changing this pattern is the HC Labs owner's
# decision, so the PR that changes it must quote the owner's words.
set -eu

tag=${1-}
if ! printf '%s' "$tag" | grep -Eq '^v(0\.0\.(0|[1-9][0-9]*)|[1-9][0-9]*\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*))$'; then
  echo "::error::'$tag' is not a release tag. Tag vMAJOR.MINOR.PATCH from v1.0.0 (Semantic Versioning, no pre-release suffix)." >&2
  exit 1
fi
