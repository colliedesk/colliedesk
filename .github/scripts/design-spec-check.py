#!/usr/bin/env python3
"""Fails when a pull request works an issue that carries a design spec and does not say what it
did with that spec.

An issue carries a design spec when one of its comments starts with "Design spec". A pull request
works an issue when its description says "Closes #N", "Fixes #N", "Resolves #N", "Part of #N" or
"Refs #N" (or the same with the issue's full URL). Such a description needs a `## Design spec`
section whose items each start with a status and say something after it:

    - done: test chat beside the switch
    - changed: no audience row, because every visitor sees the chat today
    - not in this PR: Pause after going live (#410)

Usage: design-spec-check.py BODY_FILE. Comments are read with `gh api`; tests set
DESIGN_SPEC_COMMENTS_FILE to a JSON file {"<number>": ["<comment body>", ...]} instead.
"""
import json
import os
import re
import subprocess
import sys

REPO = os.environ.get("DESIGN_SPEC_REPO", "colliedesk/colliedesk")
REFERENCE = re.compile(
    r"(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?|part\s+of|refs?)\s+"
    r"(?:#|https://github\.com/" + re.escape(REPO) + r"/issues/)(\d+)")
STATUS = re.compile(r"(?i)^-\s+(done|changed|not in this PR):\s*(.*)$")
SECTION = "## Design spec"


def fetch(number):
    out = subprocess.run(["gh", "api", "--paginate", f"repos/{REPO}/issues/{number}/comments",
                          "--jq", ".[].body | @json"], capture_output=True, text=True)
    if out.returncode != 0:
        sys.exit(f"cannot read the comments of #{number}: {out.stderr.strip()}")
    return [json.loads(line) for line in out.stdout.splitlines() if line.strip()]


def lookup():
    path = os.environ.get("DESIGN_SPEC_COMMENTS_FILE")
    if not path:
        return fetch
    with open(path, encoding="utf-8") as f:
        comments = json.load(f)
    return lambda number: comments.get(str(number), [])


def section(body):
    """Returns the lines under `## Design spec` up to the next heading, or None."""
    lines, inside = [], False
    for line in body.splitlines():
        if line.strip().lower() == SECTION.lower():
            inside = True
            continue
        if inside and re.match(r"#{1,6}\s", line):
            break
        if inside:
            lines.append(line)
    return lines if inside else None


def check(body, get_comments):
    body = re.sub(r"<!--.*?-->", "", body, flags=re.S)  # the PR template's example is a comment
    with_spec = [n for n in sorted(set(REFERENCE.findall(body)), key=int)
                 if any(c.lstrip().lower().startswith("design spec") for c in get_comments(n))]
    if not with_spec:
        return []
    issues = ", ".join(f"#{n}" for n in with_spec)
    lines = section(body)
    if lines is None:
        return [f"{issues} carries a design spec (a comment starting \"Design spec\"). Add a "
                f"`{SECTION}` section to the PR description with one item per mechanism: "
                "`- done: …`, `- changed: … because …` or `- not in this PR: … (#issue)`."]
    errors, items = [], 0
    for line in lines:
        if not line.lstrip().startswith("- "):
            continue
        match = STATUS.match(line.strip())
        if not match:
            errors.append(f"`{line.strip()}` needs a status: done:, changed: or not in this PR:")
        elif not match.group(2).strip():
            errors.append(f"`{line.strip()}` says nothing after `{match.group(1)}:`")
        items += 1
    if items == 0:
        errors.append(f"`{SECTION}` has no item; list each mechanism of the spec on {issues}.")
    return errors


def main():
    if len(sys.argv) != 2:
        sys.exit("usage: design-spec-check.py BODY_FILE")
    with open(sys.argv[1], encoding="utf-8") as f:
        body = f.read()
    errors = check(body, lookup())
    for error in errors:
        print(f"design spec: {error}")
    if errors:
        print("Edit the PR description, then re-run the failed job: gh run rerun RUN_ID --failed")
        sys.exit(1)
    print("design spec: ok")


if __name__ == "__main__":
    main()
