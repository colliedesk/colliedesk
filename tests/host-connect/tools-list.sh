#!/bin/sh
# Lists the tools of a Colliedesk twice, over HTTP and through `collie mcp`, and fails when the two
# lists differ: the bridge must show a stdio host exactly what the server has. Needs curl, jq and a
# collie binary (cmd/collie-client). No model and no API key.
#   COLLIEDESK_TOKEN=cdk_agent_... sh tests/host-connect/tools-list.sh http://localhost:8080 ./collie
# The token stays in the environment and a 0600 header file, never in an argument.
set -eu
origin=${1:?usage: tools-list.sh ORIGIN COLLIE}
collie=${2:?usage: tools-list.sh ORIGIN COLLIE}
: "${COLLIEDESK_TOKEN:?set COLLIEDESK_TOKEN to an agent token}"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
(umask 077 && printf 'Authorization: Bearer %s\n' "$COLLIEDESK_TOKEN" > "$work/header")

initialize='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"host-connect","version":"0"}}}'
list='{"jsonrpc":"2.0","id":2,"method":"tools/list"}'

post() {
  curl -fsS "$origin/mcp" -H @"$work/header" -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' -H 'MCP-Protocol-Version: 2025-11-25' --data "$1"
}
post "$initialize" > /dev/null
post "$list" | jq -r '.result.tools[].name' | sort > "$work/http"

start=$(date +%s)
printf '%s\n%s\n' "$initialize" "$list" | "$collie" mcp --url "$origin" 2> "$work/stderr" |
  jq -r 'select(.id == 2) | .result.tools[].name' | sort > "$work/bridge"
seconds=$(( $(date +%s) - start ))

if grep -qF "$COLLIEDESK_TOKEN" "$work/stderr"; then
  echo 'host-connect: the token appeared on the stderr of collie mcp' >&2; exit 1
fi
if ! diff -u "$work/http" "$work/bridge"; then
  echo 'host-connect: collie mcp lists other tools than /mcp' >&2; exit 1
fi
echo "host-connect: $(wc -l < "$work/http" | tr -d ' ') tools over HTTP and through collie mcp (${seconds}s)"
