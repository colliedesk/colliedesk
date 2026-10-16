#!/usr/bin/env python3
"""ROADMAP.md promises only what the issue tracker backs: Next/Then links are open `planned`
issues, Later links are open `accepted` or `planned` issues. Runs the real script with the issue
lookup replaced by a JSON file (ROADMAP_ISSUES_FILE), so no network is used."""
import json
import os
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CHECK = os.path.join(ROOT, ".github", "scripts", "roadmap-check.py")
REPO = "https://github.com/colliedesk/colliedesk/issues/"


def run(roadmap, issues, closing=""):
    with tempfile.TemporaryDirectory() as tmp:
        path = os.path.join(tmp, "ROADMAP.md")
        with open(path, "w", encoding="utf-8") as f:
            f.write(roadmap)
        issues_path = os.path.join(tmp, "issues.json")
        with open(issues_path, "w", encoding="utf-8") as f:
            json.dump(issues, f)
        env = dict(os.environ, ROADMAP_ISSUES_FILE=issues_path, ROADMAP_CLOSING=closing)
        return subprocess.run([sys.executable, "-I", CHECK, path], env=env,
                              capture_output=True, text=True)


def doc(next_items="", then_items="", later_items="", now_items=""):
    return ("# Roadmap\n\n## Now: alpha (0.0.x)\n\n" + now_items +
            "\n## Next: beta (0.1.0)\n\n" + next_items +
            "\n## Then: AI included\n\n" + then_items +
            "\n## Later\n\n" + later_items + "\nHave a need?\n")


def issue(state="open", labels=()):
    return {"state": state, "labels": list(labels)}


CASES = [
    # (name, roadmap, issues, should_pass, expected text in output[, issues the PR closes])
    ("planned issue in Next passes",
     doc(next_items=f"- Import ([#97]({REPO}97)).\n"), {"97": issue(labels=["planned"])}, True, ""),
    ("planned issue in Then passes",
     doc(then_items=f"- Backtest ([#99]({REPO}99)).\n"), {"99": issue(labels=["planned", "enhancement"])}, True, ""),
    ("accepted issue in Later passes",
     doc(later_items=f"- Slack ([#102]({REPO}102)).\n"), {"102": issue(labels=["accepted"])}, True, ""),
    ("accepted-only issue in Next fails",
     doc(next_items=f"- Import ([#97]({REPO}97)).\n"), {"97": issue(labels=["accepted"])}, False, "#97"),
    ("unlabelled issue in Later fails",
     doc(later_items=f"- Slack ([#102]({REPO}102)).\n"), {"102": issue(labels=["enhancement"])}, False, "#102"),
    ("closed issue fails",
     doc(then_items=f"- Drafts ([#89]({REPO}89)).\n"), {"89": issue(state="closed", labels=["planned"])}, False, "closed"),
    ("unknown issue fails",
     doc(next_items=f"- Ghost ([#999]({REPO}999)).\n"), {}, False, "#999"),
    ("links in Now are not checked",
     doc(now_items=f"- Shipped ([#5]({REPO}5)).\n"), {}, True, ""),
    ("links to other repositories are ignored",
     doc(next_items="- Site ([#31](https://github.com/colliedesk/colliedesk.com/issues/31)).\n"), {}, True, ""),
    # A pull request that closes a planned issue must take it off the roadmap in the same PR.
    ("PR closing an issue still on the roadmap fails",
     doc(next_items=f"- Import ([#97]({REPO}97)).\n"), {"97": issue(labels=["planned"])}, False,
     "closes #97", "97"),
    ("PR closing an issue already taken off the roadmap passes",
     doc(next_items=f"- Collision ([#101]({REPO}101)).\n"), {"101": issue(labels=["planned"])}, True,
     "", "97 12"),
    ("PR closing an issue listed under Now passes",
     doc(now_items=f"- Import ([#97]({REPO}97)).\n"), {}, True, "", "97"),
]

failed = 0
for name, roadmap, issues, should_pass, expect, *closing in CASES:
    r = run(roadmap, issues, *closing)
    out = r.stdout + r.stderr
    ok = (r.returncode == 0) == should_pass and (expect in out)
    if not ok:
        failed = 1
        print(f"FAIL {name}: exit={r.returncode}\n{out}", file=sys.stderr)

if failed:
    sys.exit(1)
print("roadmap gate ok")
