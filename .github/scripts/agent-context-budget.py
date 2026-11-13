#!/usr/bin/env python3
"""Refuse a PR that makes the agent instructions larger than its base.

Every AGENTS.md and CLAUDE.md in the tree loads into agent sessions, so each byte costs every
session. Growth needs the owner's agent-context-increase label (--allow-growth); moving text
between files or cutting it always passes. Compares the merge base with the PR head.

Usage: agent-context-budget.py BASE HEAD [--allow-growth]
"""
import argparse
import subprocess
import sys

NAMES = ('AGENTS.md', 'CLAUDE.md')
LABEL = 'agent-context-increase'


def git(*args):
    return subprocess.run(['git', *args], check=True, text=True, capture_output=True).stdout


def sizes(rev):
    out = {}
    for line in git('ls-tree', '-r', '-l', '-z', rev).split('\0'):
        meta, _, path = line.partition('\t')
        mode, kind, _, size = meta.split() if path else ('', '', '', '')
        # A symlink (mode 120000) points at another file and adds no text of its own.
        if path.rsplit('/', 1)[-1] in NAMES and kind == 'blob' and mode != '120000':
            out[path] = int(size)
    return out


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('base')
    parser.add_argument('head')
    parser.add_argument('--allow-growth', action='store_true')
    args = parser.parse_args()
    before = sizes(git('merge-base', args.base, args.head).strip())
    after = sizes(args.head)
    total_before, total_after = sum(before.values()), sum(after.values())
    for path in sorted(set(before) | set(after)):
        old, new = before.get(path, 0), after.get(path, 0)
        if old != new:
            print(f'{path}: {old} -> {new} ({new - old:+d})')
    print(f'agent context: {total_before} -> {total_after} bytes ({total_after - total_before:+d})')
    if total_after <= total_before:
        return 0
    if args.allow_growth:
        print(f'growth accepted by the {LABEL} label')
        return 0
    print(f'Agent instructions grew by {total_after - total_before} bytes. Cut or move text so the total does '
          f'not grow, or ask the owner to add the {LABEL} label. Agents never add that label.', file=sys.stderr)
    return 1


if __name__ == '__main__':
    sys.exit(main())
