#!/usr/bin/env python3
"""Fails when ROADMAP.md promises something the issue tracker does not back.

ROADMAP.md groups items by what they do for a team, not by date. Each item is one `- ` line under a
`## ` heading and ends with its status in backticks:
- `shipped`: live today. Not checked.
- `planned`: links at least one issue of this repository; each is open and labelled `planned`.
- `later`: links at least one issue; each is open and labelled `accepted` or `planned`.
A pull request that closes an issue (ROADMAP_CLOSING="97 101", set by CI from the PR's closing
references) must mark its item `shipped` (or drop the link) in the same PR.

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
STATUS = re.compile(r"`([^`]+)`\s*$")
RULES = {"shipped": None, "planned": {"planned"}, "later": {"accepted", "planned"}}


def items(text):
    """Yields (line number, line) for each top-level bullet under a ## heading."""
    in_section = False
    for number, line in enumerate(text.splitlines(), 1):
        if line.startswith("## "):
            in_section = True
        elif in_section and line.startswith("- "):
            yield number, line


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
    names = " ".join(f"`{name}`" for name in RULES)
    for lineno, line in items(text):
        found = STATUS.search(line)
        status = found.group(1) if found else None
        if status not in RULES:
            errors.append(f"Line {lineno}: end the item with its status, one of {names}.")
            continue
        allowed = RULES[status]
        if allowed is None:
            continue
        numbers = LINK.findall(line)
        if not numbers:
            errors.append(f"Line {lineno}: a `{status}` item must link the issue that backs it.")
        for number in numbers:
            where = f"#{number} (`{status}`, line {lineno})"
            if number in closing:
                errors.append(f"This pull request closes #{number}, which is still `{status}`. "
                              "Mark its item `shipped` in ROADMAP.md in this PR.")
                continue
            issue = get_issue(number)
            if issue is None:
                errors.append(f"{where}: issue not found in {REPO}.")
            elif issue["state"] != "open":
                errors.append(f"{where}: issue is closed. Mark the item `shipped`, or remove it.")
            elif not allowed & set(issue["labels"]):
                need = " or ".join(f"`{name}`" for name in sorted(allowed))
                errors.append(f"{where}: label the issue {need}, or give the item the status its label backs.")
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
