#!/usr/bin/env python3
"""Partition go test -list output; all tests, examples and fuzz seeds run once."""
import argparse
import re
import sys

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('shard', type=int)
parser.add_argument('total', type=int)
args = parser.parse_args()
if not 1 <= args.shard <= args.total:
    parser.error('shard must be between 1 and total')
names = sorted({line.strip() for line in sys.stdin
                if re.fullmatch(r'(Test|Example|Fuzz)\w*', line.strip())})
if not names:
    parser.error('no tests discovered; refusing an empty suite')
# Large latency fixtures seed thousands of rows. Give them one runner so the
# contract-heavy shards do not also pay that cost. Keep every threshold intact.
latency = [name for name in names if name.endswith('LatencyBudget')]
regular = [name for name in names if name not in latency]
if args.total == 1:
    selected = names
elif args.shard == args.total:
    selected = latency
else:
    selected = regular[args.shard - 1::args.total - 1]
# Anchors prevent a selected TestFoo from also running TestFooBar on another shard.
print('^(' + '|'.join(selected) + ')$' if selected else '^$')
print(f'shard {args.shard}/{args.total}: {len(selected)}/{len(names)} names', file=sys.stderr)
