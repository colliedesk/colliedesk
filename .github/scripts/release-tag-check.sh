#!/bin/sh
# Accepts only the release tags allowed today. Colliedesk is alpha: releases are v0.0.N.
# Beta (v0.1.0) starts only when the HC Labs owner announces it; changing this pattern is that
# decision, so the PR that changes it must quote the owner's announcement.
set -eu

tag=${1-}
if ! printf '%s' "$tag" | grep -Eq '^v0\.0\.(0|[1-9][0-9]*)$'; then
  echo "::error::'$tag' is not an alpha release tag. Colliedesk is alpha: tag v0.0.N. Beta (v0.1.0) needs the owner's announcement." >&2
  exit 1
fi
