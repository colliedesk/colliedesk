#!/bin/sh
# User guides (docs/user) describe the product the same way for Colliedesk Cloud and for a
# self-hosted server. Server operation (install, .env, compose, the collie CLI) belongs in
# docs/self-host; a user guide links there instead. The Freshdesk and Help Scout imports are
# migration guides run with the CLI, so they are the exceptions.
set -eu
dir=${1:-docs/user}
pattern='docker compose|/app/collie|install\.sh|\.env\b|compose\.yaml'
found=$(grep -nE "$pattern" "$dir"/*.md | grep -vE "^$dir/import-(freshdesk|helpscout)\.md:" || true)
if [ -n "$found" ]; then
  printf '%s\n' "$found"
  printf 'user-docs-check: server operation found in %s; move it to docs/self-host and link there.\n' "$dir" >&2
  exit 1
fi
