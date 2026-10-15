#!/bin/sh
# The product is spelled "Colliedesk": one capital letter. Fails when a tracked file uses the
# old camel-case spelling, so the brand stays consistent in code, docs and UI copy.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
OLD="Collie""Desk"
if git -C "$ROOT" grep -n -I "$OLD" -- .; then
  echo "write the product name as Colliedesk, not $OLD" >&2
  exit 1
fi
echo "brand spelling ok"
