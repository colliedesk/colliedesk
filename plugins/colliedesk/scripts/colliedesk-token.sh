#!/bin/sh
# Saves a new agent token for the Colliedesk plugin: /colliedesk-token <token>.
# Usage: colliedesk-token.sh <MCP address>, with the token on stdin.
# The token is checked against the person's Colliedesk first and saved only when Colliedesk accepts
# it, so a typo never replaces a working token. It reaches this script on stdin (a quoted heredoc in
# the skill, so a stray quote or $( in a paste is text, not shell), and goes on to curl and to
# `claude plugin configure` on stdin too. The shell Claude Code starts for the skill does hold the
# whole command, token included, while it runs, as everything typed in the session is kept in its
# history. This script never prints the token.
set -u

url=${1:-}
token=$(cat)

usage() {
  echo "$1"
  echo "Usage: /colliedesk-token cdk_agent_... (create the token in Colliedesk under Settings → Sessions)."
  exit 0
}

case $token in
"") usage "No token given." ;;
*"
"*) usage "Give one token only." ;;
cdk_agent_*) ;;
*) usage "That is not a Colliedesk agent token: it starts with cdk_agent_." ;;
esac
# Tokens are URL-safe base64 after the prefix; anything else is a paste error and could break the
# JSON or the curl config below.
rest=${token#cdk_agent_}
if [ -z "$rest" ] || [ -n "$(printf '%s' "$rest" | tr -d 'A-Za-z0-9_-')" ]; then
  usage "That token is incomplete or has extra characters; copy it again."
fi

case $url in
http://* | https://*) ;;
*)
  echo "The plugin has no Colliedesk address yet, so the token was not saved."
  echo "Set it with /plugin configure colliedesk@colliedesk: your Colliedesk address ending in /mcp, for example https://desk.example.com/mcp."
  exit 0
  ;;
esac

# The plugin id is <name>@<marketplace>; an installed plugin lives in .../cache/<marketplace>/<name>/<version>.
root=${CLAUDE_PLUGIN_ROOT:-$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)}
plugin=colliedesk@colliedesk
case $root in
*/cache/*/*/*)
  version_dir=${root%/}
  name_dir=${version_dir%/*}
  market_dir=${name_dir%/*}
  plugin="${name_dir##*/}@${market_dir##*/}"
  ;;
esac

status=$(printf 'header = "Authorization: Bearer %s"\n' "$token" | curl -K - -s -o /dev/null -w '%{http_code}' \
  --max-time 15 -X POST \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"colliedesk-token","version":"1"}}}' \
  "$url") || status=000

case $status in
2??) ;;
401 | 403)
  echo "Colliedesk refused this token (revoked, expired or from another workspace). Nothing was saved."
  echo "Create a new one under Settings → Sessions and run /colliedesk-token again."
  exit 0
  ;;
000)
  echo "Could not reach $url to check the token. Nothing was saved."
  echo "Check the address with /plugin configure $plugin, or try again when Colliedesk is up."
  exit 0
  ;;
*)
  echo "Colliedesk answered $status at $url, so the token could not be checked. Nothing was saved."
  exit 0
  ;;
esac

if ! printf '{"token":"%s"}' "$token" | claude plugin configure "$plugin" --values-stdin >/dev/null 2>&1; then
  echo "Colliedesk accepted the token, but Claude Code could not save it for $plugin."
  echo "Save it with /plugin configure $plugin instead."
  exit 0
fi
echo "Saved the new token for $plugin; Colliedesk accepted it."
echo "Run /reload-plugins to reconnect the Colliedesk MCP server with it."
