#!/bin/sh
# Opens one issue to record in AGENTS.md what a closed milestone (a goal) made true.
# Usage: milestone-agents-md.sh MILESTONE_NUMBER
# Env: REPO (owner/name), ASSIGNEE (optional), DRY_RUN=1 prints instead of filing, GH (default gh).
set -eu

number=${1:-}
[ -n "$number" ] || { echo "usage: milestone-agents-md.sh MILESTONE_NUMBER" >&2; exit 2; }
GH=${GH:-gh}

goal=$("$GH" api "repos/$REPO/milestones/$number" --jq .title)
title="AGENTS.md: record what \"$goal\" made true"

if "$GH" issue list --repo "$REPO" --state all --search "in:title \"$goal\" AGENTS.md" --limit 50 \
    --json title --jq '.[].title' | grep -Fqx "$title"; then
  echo "already filed: $title"
  exit 0
fi

body="The milestone \"$goal\" is closed. AGENTS.md is what every contributor and agent reads first, so
what this goal made true must be written there, or the next change can undo it.

- [ ] Read the issues closed in the milestone: https://github.com/$REPO/milestone/$number?closed=1
- [ ] Each guarantee the goal made true becomes a rule in AGENTS.md that names its gate (test or CI job).
- [ ] Text the goal made stale is updated: \"tracked in #N\" becomes the gate, finished journeys join their list.
- [ ] The PR that updates AGENTS.md says \"Closes\" this issue."

if [ "${DRY_RUN:-0}" = 1 ]; then
  printf 'would file: %s\nassignee: %s\n%s\n' "$title" "${ASSIGNEE:-none}" "$body"
  exit 0
fi

set -- --repo "$REPO" --title "$title" --body "$body" --label accepted
[ -n "${ASSIGNEE:-}" ] && set -- "$@" --assignee "$ASSIGNEE"
"$GH" issue create "$@"
