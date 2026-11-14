#!/bin/sh
# Fails when CLA.md differs from the gist CLA Assistant shows contributors, so people never agree to
# a text other than the one in this repository. Change both together: edit CLA.md, then
#   gh gist edit e4bbd1db04ad34c53448fcc757b7c005 -f CLA.md CLA.md
# CLA Assistant treats each gist revision as a new version of the agreement.
# Usage: tests/cla-gist-test.sh [path/to/CLA.md]   (needs gh, logged in or GH_TOKEN set)
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
LOCAL=${1:-$ROOT/CLA.md}
GIST=e4bbd1db04ad34c53448fcc757b7c005

remote=$(gh api "gists/$GIST" --jq '.files["CLA.md"].content')
if [ "$remote" != "$(cat "$LOCAL")" ]; then
  echo "CLA.md differs from the CLA Assistant gist https://gist.github.com/hiimchinh/$GIST" >&2
  echo "Update the gist in the same change: gh gist edit $GIST -f CLA.md CLA.md" >&2
  exit 1
fi
echo "CLA.md matches the CLA Assistant gist"
