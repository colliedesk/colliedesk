#!/bin/sh
# User guides are read by support staff. .github/scripts/user-docs-check.sh keeps server operation
# out of them, and keeps API detail (/api/v1/ paths, Idempotency-Key) in each guide's closing
# "## For developers and AI agents" section. Runs the real script on small fixture guides.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
SCRIPT="$ROOT/.github/scripts/user-docs-check.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
failed=0

# guide NAME: writes stdin to a fresh fixture directory named NAME and prints that directory.
guide() {
  mkdir -p "$TMP/$1"
  cat > "$TMP/$1/guide.md"
  printf '%s' "$TMP/$1"
}

# expect pass|fail DIR WANT: runs the check on DIR; WANT is text its output must contain.
expect() {
  out=$(sh "$SCRIPT" "$2" 2>&1) && status=pass || status=fail
  if [ "$status" != "$1" ]; then
    printf 'expected %s on %s, got %s:\n%s\n' "$1" "$2" "$status" "$out" >&2; failed=1
    return
  fi
  case "$out" in
    *"$3"*) ;;
    *) printf 'expected output containing "%s" on %s, got:\n%s\n' "$3" "$2" "$out" >&2; failed=1 ;;
  esac
}

dir=$(guide api-at-the-end <<'MD'
# Tags

## Add a tag

Select **Add tag** and type its name.

## For developers and AI agents

- `PATCH /api/v1/tags/{id}` renames a tag.
- Send an `Idempotency-Key` header to retry safely.

```sh
## a comment in a code block does not end the section
curl https://desk.example.com/api/v1/tags
```
MD
)
expect pass "$dir" ""

dir=$(guide api-in-a-step <<'MD'
# Tags

## Rename a tag

1. Open **Settings → Tags**.
2. Call `PATCH /api/v1/tags/{id}` with the new name.

## For developers and AI agents

The MCP tool is `colliedesk_tag_update`.
MD
)
expect fail "$dir" "$dir/guide.md:6: "
expect fail "$dir" 'For developers and AI agents'

dir=$(guide idempotency-key-in-prose <<'MD'
# Replies

A retry with the same Idempotency-Key sends one reply.
MD
)
expect fail "$dir" "$dir/guide.md:3: "

dir=$(guide section-not-last <<'MD'
# Tags

## For developers and AI agents

- `GET /api/v1/tags` lists tags.

## Limits

A tag name is at most 50 characters.
MD
)
expect fail "$dir" "$dir/guide.md:7: "
expect fail "$dir" 'last section'

mkdir -p "$TMP/exempt"
cat > "$TMP/exempt/mcp.md" <<'MD'
# Connect an AI agent (MCP)

Every tool calls a route such as `GET /api/v1/conversations`.
MD
expect pass "$TMP/exempt" ""

dir=$(guide server-operation <<'MD'
# Email

Run `docker compose up -d` on the server.
MD
)
expect fail "$dir" 'docs/self-host'

[ "$failed" = 0 ] && echo "user docs check ok"
exit "$failed"
