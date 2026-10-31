#!/bin/sh
# A closed milestone opens one issue to record in AGENTS.md what that goal made true.
# gh is a stub: it answers the milestone title and whether that issue already exists.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
SCRIPT="$ROOT/.github/scripts/milestone-agents-md.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
failed=0

cat > "$TMP/gh" <<'STUB'
#!/bin/sh
case "$*" in
  "api repos/o/r/milestones/3 --jq .title") echo "Goal: a small Freshdesk team switches without losing anything" ;;
  "issue list"*) cat "$STUB_EXISTING" ;;
  *) echo "unexpected gh call: $*" >&2; exit 9 ;;
esac
STUB
chmod +x "$TMP/gh"

run() { GH="$TMP/gh" REPO=o/r ASSIGNEE=closer DRY_RUN=1 STUB_EXISTING="$TMP/existing" sh "$SCRIPT" 3; }

: > "$TMP/existing"
out=$(run) || { echo "script failed when no issue exists" >&2; failed=1; }
case "$out" in
  *'would file: AGENTS.md: record what "Goal: a small Freshdesk team switches without losing anything" made true'*'assignee: closer'*) ;;
  *) echo "expected a new issue for milestone 3, got: $out" >&2; failed=1 ;;
esac

echo 'AGENTS.md: record what "Goal: a small Freshdesk team switches without losing anything" made true' > "$TMP/existing"
out=$(run) || { echo "script failed when the issue exists" >&2; failed=1; }
case "$out" in
  *"already filed"*) ;;
  *) echo "expected no second issue, got: $out" >&2; failed=1 ;;
esac

if GH="$TMP/gh" REPO=o/r DRY_RUN=1 sh "$SCRIPT" "" >/dev/null 2>&1; then
  echo "expected a missing milestone number to be refused" >&2; failed=1
fi

[ "$failed" = 0 ] && echo "milestone AGENTS.md issue ok"
exit "$failed"
