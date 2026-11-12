#!/usr/bin/env python3
"""Everyone whose work is in a pull request must have agreed to CLA.md, except the HC Labs team and
bots. Runs the real script with the pull request replaced by a JSON file (CLA_PR_FILE), so no
network is used."""
import json
import os
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CHECK = os.path.join(ROOT, ".github", "scripts", "cla-check.py")
SIGN = "I have read the Colliedesk CLA and I agree to it."


def pr(author, commits, comments=()):
    """commits: [(login or None, email)], comments: [(login, body)]."""
    return {"author": author,
            "commits": [{"login": login, "email": email} for login, email in commits],
            "comments": [{"login": login, "body": body} for login, body in comments]}


def run(data):
    with tempfile.TemporaryDirectory() as tmp:
        path = os.path.join(tmp, "pr.json")
        with open(path, "w", encoding="utf-8") as f:
            json.dump(data, f)
        env = dict(os.environ, CLA_PR_FILE=path)
        return subprocess.run([sys.executable, "-I", CHECK, "123"], env=env,
                              capture_output=True, text=True)


CASES = [
    # (name, pull request, should_pass, text the output must contain)
    ("the team's own pull request passes",
     pr("hiimchinh", [("hiimchinh", "a@example.com"), ("buiduclong0511", "b@example.com")]), True, "ok"),
    ("a bot's pull request passes",
     pr("dependabot[bot]", [("dependabot[bot]", "bot@example.com")]), True, "ok"),
    ("an outside author who has not agreed fails",
     pr("jane", [("jane", "jane@example.com")]), False, "@jane"),
    ("the failure quotes the sentence to post",
     pr("jane", [("jane", "jane@example.com")]), False, SIGN),
    ("the failure points to the agreement",
     pr("jane", [("jane", "jane@example.com")]), False, "CLA.md"),
    ("an outside author who posted the sentence passes",
     pr("jane", [("jane", "jane@example.com")], [("jane", SIGN)]), True, "ok"),
    ("the sentence passes without its period and in other case",
     pr("jane", [("jane", "jane@example.com")], [("jane", "  i have read the colliedesk cla and i agree to it \n")]),
     True, "ok"),
    ("the sentence posted by someone else does not count",
     pr("jane", [("jane", "jane@example.com")], [("hiimchinh", SIGN)]), False, "@jane"),
    ("the sentence inside a longer comment does not count",
     pr("jane", [("jane", "jane@example.com")], [("jane", SIGN + " Except section 3.")]), False, "@jane"),
    ("a co-author who has not agreed fails though the opener did",
     pr("jane", [("jane", "jane@example.com"), ("joe", "joe@example.com")], [("jane", SIGN)]), False, "@joe"),
    ("a team member's pull request with an outside commit fails",
     pr("hiimchinh", [("hiimchinh", "a@example.com"), ("joe", "joe@example.com")]), False, "@joe"),
    ("a commit no GitHub account claims fails and names its address",
     pr("jane", [(None, "ghost@example.com")], [("jane", SIGN)]), False, "ghost@example.com"),
]


def main():
    failed = 0
    for name, data, should_pass, expected in CASES:
        result = run(data)
        output = result.stdout + result.stderr
        ok = (result.returncode == 0) == should_pass and expected in output
        print(f"{'ok  ' if ok else 'FAIL'} {name}")
        if not ok:
            failed += 1
            print(f"     exit {result.returncode}, output:\n{output}")
    if failed:
        sys.exit(f"{failed} of {len(CASES)} cases failed")


if __name__ == "__main__":
    main()
