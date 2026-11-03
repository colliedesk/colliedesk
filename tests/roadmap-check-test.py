#!/usr/bin/env python3
"""ROADMAP.md promises only what the issue tracker backs. Every item carries a status: a
`planned` item links open `planned` issues, a `later` item links open `accepted` or `planned`
issues, a `shipped` item is not checked. Runs the real script with the issue lookup replaced by a
JSON file (ROADMAP_ISSUES_FILE), so no network is used."""
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


def doc(*items):
    return ("# Roadmap\n\nIntro line.\n\n## leave Freshdesk without losing anything\n\n" +
            "".join(item + "\n" for item in items) + "\nHave a need?\n")


def item(status, number=None, repo=REPO):
    link = f" ([#{number}]({repo}{number}))" if number else ""
    return f"- **Thing**: what it does for you{link}. `{status}`"


def issue(state="open", labels=()):
    return {"state": state, "labels": list(labels)}


CASES = [
    # (name, roadmap, issues, should_pass, expected text in output[, issues the PR closes])
    ("planned item on a planned issue passes",
     doc(item("planned", 97)), {"97": issue(labels=["planned"])}, True, ""),
    ("later item on an accepted issue passes",
     doc(item("later", 102)), {"102": issue(labels=["accepted"])}, True, ""),
    ("later item on a planned issue passes",
     doc(item("later", 99)), {"99": issue(labels=["planned", "enhancement"])}, True, ""),
    ("planned item on an accepted-only issue fails",
     doc(item("planned", 97)), {"97": issue(labels=["accepted"])}, False, "#97"),
    ("later item on an unlabelled issue fails",
     doc(item("later", 102)), {"102": issue(labels=["enhancement"])}, False, "#102"),
    ("closed issue fails",
     doc(item("planned", 89)), {"89": issue(state="closed", labels=["planned"])}, False, "closed"),
    ("unknown issue fails",
     doc(item("later", 999)), {}, False, "#999"),
    ("planned item without an issue fails",
     doc(item("planned")), {}, False, "link"),
    ("later item without an issue fails",
     doc(item("later")), {}, False, "link"),
    ("shipped items are not checked",
     doc(item("shipped", 5), item("shipped")), {}, True, ""),
    ("item without a status fails",
     doc("- **Thing**: what it does for you."), {}, False, "status"),
    ("item with an unknown status fails",
     doc(item("in progress", 97)), {"97": issue(labels=["planned"])}, False, "status"),
    ("links to other repositories are ignored",
     doc(item("shipped"), "- **Site**: ([#31](https://github.com/colliedesk/colliedesk.com/issues/31)). `shipped`"),
     {}, True, ""),
    ("lines outside a section are not items",
     "# Roadmap\n\n- an intro bullet\n\n## inbox\n\n" + item("shipped") + "\n", {}, True, ""),
    # A pull request that closes an issue must mark its item shipped in the same PR.
    ("PR closing an issue still planned fails",
     doc(item("planned", 97)), {"97": issue(labels=["planned"])}, False, "closes #97", "97"),
    ("PR closing an issue no longer on the roadmap passes",
     doc(item("planned", 101)), {"101": issue(labels=["planned"])}, True, "", "97 12"),
    ("PR closing an issue whose item is shipped passes",
     doc(item("shipped", 97)), {}, True, "", "97"),
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
