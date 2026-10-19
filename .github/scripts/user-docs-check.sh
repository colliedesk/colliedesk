#!/bin/sh
# User guides (docs/user) describe the product the same way for Colliedesk Cloud and for a
# self-hosted server. Server operation (install, .env, compose, the collie CLI) belongs in
# docs/self-host; a user guide links there instead. The Freshdesk import is a migration guide
# run with the CLI, so it is the one exception.
set -eu
dir=${1:-docs/user}
pattern='docker compose|/app/collie|install\.sh|\.env\b|compose\.yaml'
found=$(grep -nE "$pattern" "$dir"/*.md | grep -v "^$dir/import-freshdesk.md:" || true)
if [ -n "$found" ]; then
  printf '%s\n' "$found"
  printf 'user-docs-check: server operation found in %s; move it to docs/self-host and link there.\n' "$dir" >&2
  exit 1
fi
