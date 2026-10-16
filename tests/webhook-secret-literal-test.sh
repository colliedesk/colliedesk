#!/bin/sh
# GitHub secret scanning reads any "whsec_" followed by a long base64 run as a live Stripe webhook
# signing secret, so a literal test fixture raises a public alert on every push. Tests build the
# secret at run time instead: "whsec_" + base64.StdEncoding.EncodeToString([]byte(key)).
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
PREFIX="whsec""_"
if git -C "$ROOT" grep -n -I -E "${PREFIX}[A-Za-z0-9+/]{24,}" -- .; then
  echo "do not commit a ${PREFIX} secret literal; build it from its key at run time" >&2
  exit 1
fi
echo "no webhook secret literals"
