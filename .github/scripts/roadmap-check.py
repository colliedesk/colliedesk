#!/usr/bin/env python3
"""Fails when ROADMAP.md promises something the issue tracker does not back.

Rules, per section of ROADMAP.md:
- "Next" and "Then": every linked issue is open and labelled `planned`.
- "Later": every linked issue is open and labelled `accepted` or `planned`.
- "Now" (already shipped) is not checked.
- A pull request that closes an issue (ROADMAP_CLOSING="97 101", set by CI from the PR's closing
  references) must take that issue's link out of Next, Then and Later in the same PR.

Only links to issues of this repository count. Issues are read with `gh api`; tests set
ROADMAP_ISSUES_FILE to a JSON file {"<number>": {"state": ..., "labels": [...]}} instead.
"""
import json
import os
import re
import subprocess
import sys

REPO = os.environ.get("ROADMAP_REPO", "colliedesk/colliedesk")
LINK = re.compile(r"https://github\.com/" + re.escape(REPO) + r"/issues/(\d+)")
RULES = {"next": {"planned"}, "then": {"planned"}, "later": {"accepted", "planned"}}


def sections(text):
    """Yields (section key, line) for each line, keyed by the first word of its ## heading."""
    key = None
    for line in text.splitlines():
        if line.startswith("## "):
            key = line[3:].split(":")[0].strip().lower()
            continue
        yield key, line


def fetch(number):
    """Returns {"state", "labels"} for an issue, or None when it does not exist."""
    out = subprocess.run(["gh", "api", f"repos/{REPO}/issues/{number}"],
                         capture_output=True, text=True)
    if out.returncode != 0:
        return None
    data = json.loads(out.stdout)
    if "pull_request" in data:
        return None
    return {"state": data["state"], "labels": [label["name"] for label in data["labels"]]}


def lookup():
    path = os.environ.get("ROADMAP_ISSUES_FILE")
    if not path:
        return fetch
    with open(path, encoding="utf-8") as f:
        issues = json.load(f)
    return lambda number: issues.get(str(number))


def check(text, get_issue, closing=()):
    errors = []
    for key, line in sections(text):
        allowed = RULES.get(key)
        if not allowed:
            continue
        for number in LINK.findall(line):
            if number in closing:
                errors.append(f'This pull request closes #{number}, which is still under "{key.capitalize()}". '
                              "Remove its line from ROADMAP.md in this PR (or move it under Now without the link).")
                continue
            issue = get_issue(number)
            where = f'#{number} in "{key.capitalize()}"'
            if issue is None:
                errors.append(f"{where}: issue not found in {REPO}.")
            elif issue["state"] != "open":
                errors.append(f"{where}: issue is closed. Remove its line, or move it under Now without the link.")
            elif not allowed & set(issue["labels"]):
                need = " or ".join(f"`{name}`" for name in sorted(allowed))
                errors.append(f"{where}: label it {need}, or move the item to the section that matches its label.")
    return errors


def main():
    path = sys.argv[1] if len(sys.argv) > 1 else "ROADMAP.md"
    with open(path, encoding="utf-8") as f:
        closing = set(os.environ.get("ROADMAP_CLOSING", "").split())
        errors = check(f.read(), lookup(), closing)
    for error in errors:
        print(f"::error file={path}::{error}")
    if errors:
        sys.exit(1)
    print("ROADMAP.md matches the issue tracker")


if __name__ == "__main__":
    main()
