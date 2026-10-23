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
parser.add_argument('--latency-shards', type=int, default=1,
                    help='how many of the last shards run the latency fixtures')
parser.add_argument('--latency-share', type=float, default=0,
                    help='regular tests a latency shard runs after its gates, as a fraction of a regular shard')
args = parser.parse_args()
if not 1 <= args.shard <= args.total:
    parser.error('shard must be between 1 and total')
if args.parts < 1:
    parser.error('parts must be at least 1')
if args.total > 1 and not 1 <= args.latency_shards < args.total:
    parser.error('latency shards must be at least 1 and leave a regular shard')
if args.latency_share < 0:
    parser.error('latency share cannot be negative')
names = sorted({line.strip() for line in sys.stdin
                if re.fullmatch(r'(Test|Example|Fuzz)\w*', line.strip())})
if not names:
    parser.error('no tests discovered; refusing an empty suite')
# Large latency fixtures seed thousands of rows and time a request against a budget. Give them their
# own runners so the contract-heavy shards do not also pay that cost, and so the packages those shards run
# at the same time do not load the shared database while they measure. Keep every threshold intact.
# They are named ...LatencyBudget, ...StaysInBudget or ...StaysFast...; beside other packages'
# tests one measured 389 ms against 200 ms, another a 266 ms p95 against 100 ms.
TIMED = ('LatencyBudget', 'StaysInBudget', 'StaysFast')
latency = [name for name in names if any(marker in name for marker in TIMED)]
regular = [name for name in names if name not in latency]
# httpapi runs its gates one after another (66 seconds on one runner in CI), so they spread over
# several latency runners. The gates that build the shared knowledge-base seed (sharedKBBudget in
# httpapi) stay on the first, so the seed is built once; the others take turns on the rest.
KB_SEED = ('HelpCenterPages', 'KnowledgeBase', 'KBMedia')
kb = [name for name in latency if any(marker in name for marker in KB_SEED)]
rest = [name for name in latency if name not in kb]
regular_total = args.total - args.latency_shards
# A latency shard ends 30 to 60 seconds before the regular ones, so it can take a share of the regular
# tests once its gates are done. Smooth weighted round robin over the sorted names: each regular shard
# weighs 1 and each latency shard the share, so every name has exactly one shard.
weights = [1.0] * regular_total + [args.latency_share] * (args.total - regular_total)
if args.total == 1 or args.latency_share == 0:
    weights = [1.0] * regular_total + [0.0] * (args.total - regular_total)
credit = [0.0] * args.total
owner = {}
for name in regular:
    credit = [c + w for c, w in zip(credit, weights)]
    best = max(range(args.total), key=lambda i: (credit[i], -i))
    credit[best] -= sum(weights)
    owner[name] = best + 1
mine = [name for name in regular if owner[name] == args.shard]
if args.total == 1:
    gates = []
    mine = names
elif args.shard <= regular_total:
    gates = []
elif args.latency_shards == 1:
    gates = latency
else:
    index = args.shard - regular_total - 1
    gates = kb if index == 0 else rest[index - 1::args.latency_shards - 1]
# A regular shard's tests mostly wait on PostgreSQL, one test at a time in each package binary, and
# httpapi takes most of the shard: the job runs one go test per part at once. A latency shard runs
# its gates as one process with no other test binary of the shard beside them; only after they
# finished ('--' tells the job to wait) does its share of regular tests run in parts.
lines = []
if gates:
    lines.append('^(' + '|'.join(gates) + ')$')
if mine or not gates:
    if gates:
        lines.append('--')
    for part in range(args.parts):
        chosen = mine[part::args.parts]
        # Anchors prevent a selected TestFoo from also running TestFooBar on another shard.
        lines.append('^(' + '|'.join(chosen) + ')$' if chosen else '^$')
print('\n'.join(lines))
print(f'shard {args.shard}/{args.total}: {len(gates) + len(mine)}/{len(names)} names, {len(gates)} gates, '
      f'{len(mine)} regular in {args.parts} part(s)', file=sys.stderr)
