#!/usr/bin/env python3
"""A pull request that works an issue carrying a "Design spec" comment must say, per mechanism,
what it did with that spec. Runs the real script with the issue comments replaced by a JSON file
(DESIGN_SPEC_COMMENTS_FILE), so no network is used."""
import json
import os
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CHECK = os.path.join(ROOT, ".github", "scripts", "design-spec-check.py")
SPEC = "Design spec, inlined so nothing outside this issue is needed.\n\n### One page\n..."
GOOD = ("## Design spec\n\n"
        "- done: test chat beside the switch\n"
        "- changed: no audience row, because every visitor sees the chat today\n"
        "- not in this PR: Pause after going live (#410)\n")


def run(body, comments):
    with tempfile.TemporaryDirectory() as tmp:
        body_path = os.path.join(tmp, "body.md")
        with open(body_path, "w", encoding="utf-8") as f:
            f.write(body)
        comments_path = os.path.join(tmp, "comments.json")
        with open(comments_path, "w", encoding="utf-8") as f:
            json.dump(comments, f)
        env = dict(os.environ, DESIGN_SPEC_COMMENTS_FILE=comments_path)
        return subprocess.run([sys.executable, "-I", CHECK, body_path], env=env,
                              capture_output=True, text=True)


CASES = [
    # (name, PR body, {issue: [comment bodies]}, should_pass, expected text in output)
    ("no issue referenced passes", "## What and why\n\nTidy a test.\n", {}, True, ""),
    ("issue without a design spec passes", "Closes #97\n", {"97": ["Looks good to me."]}, True, ""),
    ("a comment that only mentions a design spec does not count",
     "Closes #97\n", {"97": ["We should write a design spec later."]}, True, ""),
    ("closing an issue with a spec and no section fails",
     "Closes #405\n", {"405": ["Accepted.", SPEC]}, False, "#405"),
    ("the failure names the section to add",
     "Closes #405\n", {"405": [SPEC]}, False, "## Design spec"),
    ("a phase PR that is part of an issue with a spec is checked too",
     "Part of #124\n", {"124": [SPEC]}, False, "#124"),
    ("a full issue URL counts as a reference",
     "Closes https://github.com/colliedesk/colliedesk/issues/405\n", {"405": [SPEC]}, False, "#405"),
    ("a section with done, changed and not in this PR passes",
     "Closes #405\n\n" + GOOD, {"405": [SPEC]}, True, ""),
    ("an empty section fails",
     "Closes #405\n\n## Design spec\n\n## Checklist\n- [x] Tests\n", {"405": [SPEC]}, False, "no item"),
    ("an item without a status fails",
     "Closes #405\n\n## Design spec\n\n- test chat beside the switch\n", {"405": [SPEC]}, False,
     "test chat beside the switch"),
    ("a status with nothing after it fails",
     "Closes #405\n\n## Design spec\n\n- done:\n", {"405": [SPEC]}, False, "done:"),
    ("the template's commented-out example does not count",
     "Closes #405\n\n<!-- If the issue has a design spec:\n## Design spec\n\n- done: <mechanism>\n-->\n",
     {"405": [SPEC]}, False, "#405"),
    ("the section ends at the next heading",
     "Closes #405\n\n" + GOOD + "\n## Checklist\n\n- [x] Tests written first\n", {"405": [SPEC]}, True, ""),
]


def main():
    failed = 0
    for name, body, comments, should_pass, expected in CASES:
        out = run(body, comments)
        passed = out.returncode == 0
        text = out.stdout + out.stderr
        if passed != should_pass or (expected and expected not in text):
            failed += 1
            print(f"FAIL {name}: exit {out.returncode}\n{text}")
        else:
            print(f"ok   {name}")
    if failed:
        sys.exit(f"{failed} case(s) failed")


if __name__ == "__main__":
    main()
