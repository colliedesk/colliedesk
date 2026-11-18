#!/usr/bin/env python3
"""Fails when a pull request description retells how review went instead of stating its result.

Review runs before the pull request opens. The reader needs the current state, so a line that
pairs "review" with rounds, finding counts, finding codes, severities or "separate session" is
refused. One line says who reviewed it, with the outcome:

    Reviewed by <model>, in <harness>; no open findings.

HTML comments and fenced code are not read. Usage: pr-body-review-log-check.py BODY_FILE.
"""
import re
import sys

REVIEW = re.compile(r"(?i)\b(re-?review|review)\b")
# Severities are upper case only, so "low stock" or "high volume" is ordinary prose.
LOG = re.compile(r"(?i:\bround|separate session|independent|adversarial|\b\d+\s*(findings?|issues?|bugs?)\b|"
                 r"\b(one|two|three|four|five)\s+(\w+\s+)?findings\b)|\b(HIGH|MEDIUM|LOW|CRITICAL)\b")


def readable(body):
    body = re.sub(r"<!--.*?-->", "", body.replace("\r\n", "\n"), flags=re.S)
    return re.sub(r"^(`{3,}|~{3,})[^\n]*\n.*?^\1[ \t]*$", "", body, flags=re.M | re.S)


def main(path):
    with open(path, encoding="utf-8") as f:
        lines = [line for line in readable(f.read()).splitlines() if REVIEW.search(line) and LOG.search(line)]
    if not lines:
        print("pr-body-review-log-check: OK")
        return 0
    quoted = "\n".join(f"  {line.strip()[:160]}" for line in lines)
    print("The description retells how review went (rounds, finding counts, severities):\n"
          f"{quoted}\n"
          'Review is done before the pull request opens; say who reviewed it and the result, for example '
          '"Reviewed by <model>, in <harness>; no open findings." Then re-run the roadmap job.', file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
