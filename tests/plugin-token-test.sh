#!/bin/sh
# /colliedesk-token <token> in the Claude Code plugin: checks the token against the person's
# Colliedesk, saves it only when Colliedesk accepts it, and never prints it. curl and claude are
# stubbed; the stubs record what they were given.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
SCRIPT="$ROOT/plugins/colliedesk/scripts/colliedesk-token.sh"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin" "$WORK/cache/acme-market/colliedesk/abc123"

cat >"$WORK/bin/curl" <<'EOF'
#!/bin/sh
# Reads the curl config from stdin (-K -), so the token never sits in curl's arguments.
cat >"$STUB/curl.stdin"
printf '%s\n' "$@" >"$STUB/curl.args"
[ "$CURL_STATUS" = fail ] && exit 7
printf '%s' "$CURL_STATUS"
EOF
cat >"$WORK/bin/claude" <<'EOF'
#!/bin/sh
printf '%s\n' "$@" >"$STUB/claude.args"
cat >"$STUB/claude.stdin"
exit "${CLAUDE_EXIT:-0}"
EOF
chmod +x "$WORK/bin/curl" "$WORK/bin/claude"

TOKEN="cdk_agent_""Zx9-Qw_ErTy81uIoPaSdFgHjKlZxCvBn"
URL="https://acme.colliedesk.com/mcp"
fails=0

run() { # run <expected status> <url> <token>; the token arrives on stdin, as the skill's heredoc sends it
  rm -f "$WORK/curl.stdin" "$WORK/curl.args" "$WORK/claude.args" "$WORK/claude.stdin"
  printf '%s\n' "$3" | STUB="$WORK" CURL_STATUS="$1" PATH="$WORK/bin:/usr/bin:/bin" \
    CLAUDE_PLUGIN_ROOT="$WORK/cache/acme-market/colliedesk/abc123" \
    sh "$SCRIPT" "$2" >"$WORK/out" 2>&1 || echo "exit status $?" >>"$WORK/out"
}
fail() { echo "FAIL: $1" >&2; sed 's/^/  | /' "$WORK/out" >&2; fails=$((fails + 1)); }
saved() { [ -f "$WORK/claude.args" ]; }
leaked() { grep -q "$TOKEN" "$WORK/out" || { [ -f "$WORK/curl.args" ] && grep -q "$TOKEN" "$WORK/curl.args"; }; }

# 1. Colliedesk accepts the token: it is saved for the plugin it was installed as, from stdin.
run 200 "$URL" "$TOKEN"
saved || fail "an accepted token was not saved"
grep -qx "colliedesk@acme-market" "$WORK/claude.args" 2>/dev/null || fail "saved under the wrong plugin id"
grep -qx -- "--values-stdin" "$WORK/claude.args" 2>/dev/null || fail "token not passed on stdin"
[ "$(cat "$WORK/claude.stdin" 2>/dev/null)" = "{\"token\":\"$TOKEN\"}" ] || fail "stdin is not {\"token\":...}"
grep -q "Authorization: Bearer $TOKEN" "$WORK/curl.stdin" 2>/dev/null || fail "the check did not send the token"
grep -qx "$URL" "$WORK/curl.args" 2>/dev/null || fail "the check did not call the MCP address"
grep -q "/reload-plugins" "$WORK/out" || fail "does not say how to reconnect"
leaked && fail "token printed or put on curl's command line"

# 2. Colliedesk refuses it (revoked, expired, another workspace): nothing is saved.
for status in 401 403; do
  run "$status" "$URL" "$TOKEN"
  saved && fail "a refused token ($status) was saved"
  grep -qi "refused" "$WORK/out" || fail "a refused token ($status) is not explained"
  grep -q "Settings → External AI agents" "$WORK/out" || fail "does not say where to create a token ($status)"
  leaked && fail "token printed ($status)"
done

# 3. Colliedesk cannot be reached: nothing is saved, and the reason is said.
run fail "$URL" "$TOKEN"
saved && fail "saved a token nobody checked"
grep -qi "could not reach" "$WORK/out" || fail "an unreachable server is not explained"

# 4. Not an agent token, or no token at all: nothing is checked or saved, and the usage is shown.
for bad in "" "hello" "cdk_session_abc" "cdk_agent_" "cdk_agent_abc def" "cdk_agent_a'b" \
  "cdk_agent_a\$(touch $WORK/injected)" "cdk_agent_abc
cdk_agent_def"; do
  run 200 "$URL" "$bad"
  saved && fail "saved \"$bad\""
  [ -f "$WORK/curl.args" ] && fail "checked \"$bad\" against the server"
  grep -q "/colliedesk-token cdk_agent_" "$WORK/out" || fail "no usage for \"$bad\""
  grep -q "Settings → External AI agents" "$WORK/out" || fail "usage does not say where to create a token"
  [ -e "$WORK/injected" ] && fail "ran a command from \"$bad\""
done

# 5. The plugin's address is not set: it says so instead of saving blind.
# shellcheck disable=SC2016 # the literal placeholder Claude Code leaves when the address is unset
run 200 '${user_config.url}' "$TOKEN"
saved && fail "saved without an address"
grep -q "/plugin configure" "$WORK/out" || fail "missing address not explained"

# 6. Saving fails: it says so, never claims success.
export CLAUDE_EXIT=1
run 200 "$URL" "$TOKEN"
unset CLAUDE_EXIT
grep -q "/reload-plugins" "$WORK/out" && fail "claimed success after a failed save"
grep -qi "could not save" "$WORK/out" || fail "a failed save is not explained"

[ "$fails" -eq 0 ] || { echo "$fails failure(s)" >&2; exit 1; }
echo "plugin token command: ok"
