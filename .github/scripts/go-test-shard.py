#!/usr/bin/env python3
"""Partition go test -list output; all tests, examples and fuzz seeds run once."""
import argparse
import re
import sys

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('shard', type=int)
parser.add_argument('total', type=int)
parser.add_argument('--parts', type=int, default=1,
                    help='print a regular shard as this many disjoint patterns, one per line')
args = parser.parse_args()
if not 1 <= args.shard <= args.total:
    parser.error('shard must be between 1 and total')
if args.parts < 1:
    parser.error('parts must be at least 1')
names = sorted({line.strip() for line in sys.stdin
                if re.fullmatch(r'(Test|Example|Fuzz)\w*', line.strip())})
if not names:
    parser.error('no tests discovered; refusing an empty suite')
# Large latency fixtures seed thousands of rows and time a request against a budget. Give them one
# runner so the contract-heavy shards do not also pay that cost, and so the packages those shards run
# at the same time do not load the shared database while they measure. Keep every threshold intact.
# They are named ...LatencyBudget, ...StaysInBudget or ...StaysFast...; beside other packages'
# tests one measured 389 ms against 200 ms, another a 266 ms p95 against 100 ms.
TIMED = ('LatencyBudget', 'StaysInBudget', 'StaysFast')
latency = [name for name in names if any(marker in name for marker in TIMED)]
regular = [name for name in names if name not in latency]
if args.total == 1:
    selected = names
elif args.shard == args.total:
    selected = latency
else:
    selected = regular[args.shard - 1::args.total - 1]
# A regular shard's tests mostly wait on PostgreSQL, one test at a time in each package binary, and
# httpapi takes most of the shard: the job runs one go test per part at once. The latency shard is
# never split, so its gates still measure with nothing else running.
parts = args.parts if args.shard < args.total else 1
for part in range(parts):
    chosen = selected[part::parts]
    # Anchors prevent a selected TestFoo from also running TestFooBar on another shard.
    print('^(' + '|'.join(chosen) + ')$' if chosen else '^$')
print(f'shard {args.shard}/{args.total}: {len(selected)}/{len(names)} names in {parts} part(s)', file=sys.stderr)
