#!/usr/bin/env python3
"""Fails when someone whose work is in a pull request has not agreed to the Contributor License
Agreement (CLA.md).

People agree by posting, on the pull request, a comment that is exactly the sentence in SIGN (case
and a final period do not matter). The opener of the pull request and the GitHub account of every
commit author must have agreed. The HC Labs team, whose work is covered by their own agreements
with HC Labs, and bots are exempt. A commit whose address no GitHub account claims cannot be
matched to an agreement and fails.

Usage: cla-check.py PR_NUMBER. The pull request is read with `gh api`; tests set CLA_PR_FILE to a
JSON file {"author": login, "commits": [{"login", "email"}], "comments": [{"login", "body"}]}.
"""
import json
import os
import subprocess
import sys

REPO = os.environ.get("CLA_REPO", "colliedesk/colliedesk")
SIGN = "I have read the Colliedesk CLA and I agree to it."
# HC Labs staff sign an intellectual-property agreement with HC Labs instead of the CLA.
TEAM = {"hiimchinh", "buiduclong0511"}


def exempt(login):
    return login in TEAM or login.endswith("[bot]")


def agrees(body):
    return body.strip().rstrip(".").strip().lower() == SIGN.rstrip(".").lower()


def api(path, jq):
    out = subprocess.run(["gh", "api", "--paginate", f"repos/{REPO}/{path}", "--jq", jq],
                         capture_output=True, text=True)
    if out.returncode != 0:
        sys.exit(f"cannot read {path}: {out.stderr.strip()}")
    return [json.loads(line) for line in out.stdout.splitlines() if line.strip()]


def fetch(number):
    path = os.environ.get("CLA_PR_FILE")
    if path:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    return {
        "author": api(f"pulls/{number}", ".user.login | @json")[0],
        "commits": api(f"pulls/{number}/commits",
                       ".[] | {login: .author.login, email: .commit.author.email} | @json"),
        "comments": api(f"issues/{number}/comments", ".[] | {login: .user.login, body} | @json"),
    }


def check(pr):
    signed = {c["login"] for c in pr["comments"] if agrees(c["body"] or "")}
    logins = [pr["author"]] + [c["login"] for c in pr["commits"] if c["login"]]
    missing = sorted({login for login in logins if not exempt(login) and login not in signed})
    errors = [f"@{login} has not agreed to the Contributor License Agreement (CLA.md)." for login in missing]
    for commit in pr["commits"]:
        if not commit["login"]:
            errors.append(f"a commit is by {commit['email']}, which no GitHub account claims; add that "
                          "address to your GitHub account (Settings → Emails), or recommit with one it has.")
    return errors


def main():
    if len(sys.argv) != 2:
        sys.exit("usage: cla-check.py PR_NUMBER")
    errors = check(fetch(sys.argv[1]))
    for error in errors:
        print(f"cla: {error}")
    if errors:
        print(f"Read CLA.md, then post this comment on the pull request, exactly:\n\n    {SIGN}\n\n"
              "Then push a commit, or ask a maintainer to re-run the failed job.")
        sys.exit(1)
    print("cla: ok")


if __name__ == "__main__":
    main()
