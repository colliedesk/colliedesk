#!/bin/sh
# Runs the Claude Code and Codex steps of the host guides in a clean node:22 container and times
# them, the way a person on a new computer would, from an installed host to an answer of
# colliedesk_whoami. The commands come from web/src/features/settings/connect-snippets.generated.ts,
# which the guides show too. Run it by hand; it is not part of CI:
#   COLLIEDESK_TOKEN=cdk_agent_... sh tests/host-connect/hosts.sh http://host.docker.internal:8080
# With ANTHROPIC_API_KEY (Claude Code) or OPENAI_API_KEY (Codex) set, it also asks the model to call
# colliedesk_whoami; without, it checks the connection only (claude mcp list, codex mcp list).
set -eu
origin=${1:?usage: hosts.sh ORIGIN}
: "${COLLIEDESK_TOKEN:?set COLLIEDESK_TOKEN to an agent token}"
root=$(cd "$(dirname "$0")/../.." && pwd)
claude_version=${CLAUDE_CODE_VERSION:-2.1.309}
codex_version=${CODEX_VERSION:-0.163.0}

docker run --rm -i --add-host host.docker.internal:host-gateway \
  -e COLLIEDESK_TOKEN -e ANTHROPIC_API_KEY -e OPENAI_API_KEY -e ORIGIN="$origin" \
  -e CLAUDE_VERSION="$claude_version" -e CODEX_VERSION="$codex_version" \
  -v "$root/web/src/features/settings/connect-snippets.generated.ts:/snippets.ts:ro" \
  node:22-bookworm-slim sh -eu <<'INNER'
snippet() { node -e '
  const src = require("fs").readFileSync("/snippets.ts", "utf8");
  const line = src.split("\n").find((l) => l.trimStart().startsWith(process.argv[1] + ":"));
  if (!line) { console.error("no snippet " + process.argv[1]); process.exit(1); }
  const value = JSON.parse(line.slice(line.indexOf(":") + 1).trim().replace(/,$/, ""));
  console.log(value.replaceAll("https://desk.example.com", process.env.ORIGIN));' "$1"; }
npm install -g --silent "@anthropic-ai/claude-code@$CLAUDE_VERSION" "@openai/codex@$CODEX_VERSION" >/dev/null 2>&1
cd "$(mktemp -d)"

start=$(date +%s)
sh -c "$(snippet claudeCodeCLI)" >/dev/null
claude mcp list 2>&1 | grep '^colliedesk:'
if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
  claude -p --allowedTools mcp__colliedesk__colliedesk_whoami "Call colliedesk_whoami and answer with the workspace name only."
fi
echo "Claude Code: $(( $(date +%s) - start ))s"

start=$(date +%s)
sh -c "$(snippet codex)" >/dev/null
codex mcp list | grep colliedesk
if [ -n "${OPENAI_API_KEY:-}" ]; then
  printenv OPENAI_API_KEY | codex login --with-api-key >/dev/null
  codex exec --skip-git-repo-check -s read-only "Call the colliedesk_whoami tool and answer with the workspace name only."
fi
echo "Codex: $(( $(date +%s) - start ))s"
INNER
