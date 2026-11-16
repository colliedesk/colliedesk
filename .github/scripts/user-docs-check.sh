#!/bin/sh
# User guides (docs/user) are read by support staff, often on a phone. Two rules keep them about
# what a team does in the app (tests/user-docs-check-test.sh):
#
# 1. No server operation. User guides describe the product the same way for Colliedesk Cloud and
#    for a self-hosted server. Install, .env, compose and the collie CLI belong in docs/self-host;
#    a user guide links there instead. The Freshdesk and Help Scout imports are migration guides
#    run with the CLI, so they are the exceptions.
# 2. API detail only at the end. An /api/v1/ path or the Idempotency-Key header appears only in
#    the guide's closing "## For developers and AI agents" section, which is its last section.
#    Pages about the API or AI agents by nature are listed in api_page below.
set -eu
dir=${1:-docs/user}
status=0

pattern='docker compose|/app/collie|install\.sh|\.env\b|compose\.yaml'
found=$(grep -nE "$pattern" "$dir"/*.md | grep -vE "^$dir/import-(freshdesk|helpscout)\.md:" || true)
if [ -n "$found" ]; then
  printf '%s\n' "$found"
  printf 'user-docs-check: server operation found in %s; move it to docs/self-host and link there.\n' "$dir" >&2
  status=1
fi

# api_page NAME: true when the guide is about the API or AI agents, so API detail is its subject.
api_page() {
  case $1 in
    mcp.md) ;;          # the MCP server: its tools, tokens and the routes they call
    connect-*.md) ;;    # set up one AI agent host (Claude Code, Codex, Cursor, VS Code, Claude Desktop)
    agent-skills.md) ;; # skills an AI agent loads to work the inbox
    export.md) ;;       # the export file is an open format read by scripts; the page documents it
    # Rewritten in its own pull request; remove this entry when that pull request lands.
    tags.md) ;;
    *) return 1 ;;
  esac
}

section='## For developers and AI agents'
for file in "$dir"/*.md; do
  api_page "${file##*/}" && continue
  awk -v file="$file" -v section="$section" '
    /^(```|~~~)/ { fence = !fence }
    !fence && /^##? / {
      if (seen && $0 != section) {
        printf "%s:%d: a section follows \"%s\"; it must be the last section.\n", file, NR, section
        bad = 1
      }
      here = ($0 == section)
      if (here) seen = 1
    }
    !here && /\/api\/v1\/|Idempotency-Key/ {
      printf "%s:%d: API detail (an /api/v1/ path or Idempotency-Key) outside \"%s\"; user guides describe the app, so move it to that closing section.\n", file, NR, section
      bad = 1
    }
    END { exit bad }
  ' "$file" || status=1
done

[ "$status" = 0 ] || printf 'user-docs-check: fix the lines above (.github/scripts/user-docs-check.sh says why).\n' >&2
exit "$status"
