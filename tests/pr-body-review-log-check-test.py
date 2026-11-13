#!/usr/bin/env python3
"""A pull request description states the result of review, not how review went: rounds, finding
counts and severities stay out. Runs the real script on bodies taken from merged pull requests."""
import os
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CHECK = os.path.join(ROOT, ".github", "scripts", "pr-body-review-log-check.py")
TEMPLATE = os.path.join(ROOT, ".github", "pull_request_template.md")


def run(body):
    with tempfile.TemporaryDirectory() as tmp:
        path = os.path.join(tmp, "body.md")
        with open(path, "w", encoding="utf-8") as f:
            f.write(body)
        return subprocess.run([sys.executable, "-I", CHECK, path], capture_output=True, text=True)


REFUSED = (
    "4. Adversarial review in a separate session: two LOW findings; the ignored model was fixed, "
    "the lowering was dropped by the owner's decision; re-review found no bug.",
    "- Adversarial review in a separate session: 3 findings fixed, 1 accepted and documented.",
    "## Review round 2",
    "## Changed after review (ADR round 1)",
    "1. UI review F2 (LOW): each Undo in the moves log is described by its row's sentence.",
    "- Adversarial review (4 rounds) and a smoke test on 360/390/412 and 1024/1440, light and dark.",
)
ALLOWED = (
    "Review: no open findings.",
    "- Reviewed by GPT-6.1 Sol, in Codex, and by Claude Opus 5.5, each on its own",
    '**Not in this PR:** the draft card with "Review and send" (known gap).',
    "`TestPrivilegedPathsMatchTheReviewedAllowlist` keeps passing.",
    "**Copy to review:** the new paragraph is product text a model reads; please check the wording.",
    "<!-- Adversarial review in a separate session: 3 findings fixed -->",
    "```text\nReview round 2: 3 findings\n```",
    'Paper row "H3" on page 19 Help center: H3-a editor. All `[Needs review]`; please review before merge.',
    "Please review the low-stock warning copy.",
)

failures = []
for body in REFUSED:
    r = run("## What and why\n\nCloses #1.\n\n" + body + "\n")
    if r.returncode == 0 or body.lstrip("#- ")[:20] not in r.stderr:
        failures.append(f"not refused with the line quoted: {body[:50]!r} -> {r.returncode} {r.stderr.strip()}")
for body in ALLOWED + (open(TEMPLATE, encoding="utf-8").read(),):
    r = run(body + "\n")
    if r.returncode != 0:
        failures.append(f"refused: {body[:50]!r} -> {r.stderr.strip()}")

if failures:
    sys.exit("\n".join(failures))
print(f"pr-body-review-log-check: {len(REFUSED) + len(ALLOWED) + 1} cases passed")
