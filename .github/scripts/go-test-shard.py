#!/usr/bin/env python3
"""Partition go test -list output by measured time; all tests, examples and fuzz seeds run once."""
import argparse
import json
import re
import statistics
import sys

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('shard', type=int)
parser.add_argument('total', type=int)
parser.add_argument('--parts', type=int, default=1,
                    help='print a regular shard as this many disjoint patterns, one per line')
parser.add_argument('--latency-shards', type=int, default=1,
                    help='how many of the last shards run the latency fixtures')
parser.add_argument('--timings', help='JSON object of seconds per test name (go-test-timings.py writes it); '
                                      'a name it lacks weighs the median. Without it every name weighs 1')
parser.add_argument('--parallel', type=float, default=1,
                    help="how many of a part's tests run at once: its packages' test binaries overlap (go test -p), "
                         'so a part takes its summed test time divided by this; the gates count in full')
parser.add_argument('--process-seconds', type=float, default=0,
                    help='what a part with tests adds to its shard for starting its go test process')
args = parser.parse_args()
if not 1 <= args.shard <= args.total:
    parser.error('shard must be between 1 and total')
if args.parts < 1:
    parser.error('parts must be at least 1')
if args.parallel <= 0 or args.process_seconds < 0:
    parser.error('parallel must be positive and process seconds non-negative')
if args.total > 1 and not 1 <= args.latency_shards < args.total:
    parser.error('latency shards must be at least 1 and leave a regular shard')
timings = {}
if args.timings:
    try:
        with open(args.timings) as file:
            timings = json.load(file)
    except (OSError, ValueError) as error:
        parser.error(f'timings: {error}')
    if not isinstance(timings, dict) or not all(
            isinstance(seconds, (int, float)) and not isinstance(seconds, bool) and seconds >= 0
            for seconds in timings.values()):
        parser.error('timings must map test names to non-negative seconds')
names = sorted({line.strip() for line in sys.stdin
                if re.fullmatch(r'(Test|Example|Fuzz)\w*', line.strip())})
if not names:
    parser.error('no tests discovered; refusing an empty suite')
# A test added since the timings were measured weighs a typical test, not nothing.
default = statistics.median(timings.values()) if timings else 1
weight = {name: timings.get(name, default) for name in names}
# Longest first, ties by name: the partition depends only on the names and their times.
longest_first = lambda chosen: sorted(chosen, key=lambda name: (-weight[name], name))
# Large latency fixtures seed thousands of rows and time a request against a budget. Give them their
# own runners so the contract-heavy shards do not also pay that cost, and so the packages those shards run
# at the same time do not load the shared database while they measure. Keep every threshold intact.
# They are named ...LatencyBudget, ...StaysInBudget or ...StaysFast...; beside other packages'
# tests one measured 389 ms against 200 ms, another a 266 ms p95 against 100 ms.
TIMED = ('LatencyBudget', 'StaysInBudget', 'StaysFast')
latency = [name for name in names if any(marker in name for marker in TIMED)]
regular = [name for name in names if name not in latency]
regular_total = args.total - args.latency_shards
# A shard's clock: its gates in one process, one after another, then its parts at once.
gates = [[] for _ in range(args.total)]
if args.total == 1:
    regular = names
elif args.latency_shards == 1:
    gates[-1] = latency
else:
    # The gates that build the shared knowledge-base seed (sharedKBBudget in httpapi) stay on the first
    # latency runner, so the seed is built once; every other gate goes, longest first, to the latency
    # runner whose gates end first.
    KB_SEED = ('HelpCenterPages', 'KnowledgeBase', 'KBMedia')
    gates[regular_total] = [name for name in latency if any(marker in name for marker in KB_SEED)]
    for name in longest_first(name for name in latency if name not in gates[regular_total]):
        shard = min(range(regular_total, args.total), key=lambda i: (sum(weight[n] for n in gates[i]), i))
        gates[shard].append(name)
# Longest processing time first: each regular name goes to the part that would end first, so a long
# test fills a part by itself and a latency runner takes regular tests only when its gates end early.
# In run 39903507509 a regular shard's test step took about 11 s plus 0.37 times its fullest part's
# summed test times, and a latency shard's gates their summed times (the workflow's options).
parts = [[[] for _ in range(args.parts)] for _ in range(args.total)]
load = {(shard, part): sum(weight[name] for name in gates[shard])
        for shard in range(args.total) for part in range(args.parts)}
for name in longest_first(regular):
    end = lambda bin: (load[bin] + weight[name] / args.parallel
                       + (0 if parts[bin[0]][bin[1]] else args.process_seconds))
    shard, part = min(load, key=lambda bin: (end(bin), bin))
    load[shard, part] = end((shard, part))
    parts[shard][part].append(name)
mine_gates = sorted(gates[args.shard - 1])
mine = [sorted(chosen) for chosen in parts[args.shard - 1]]
# A regular shard's tests mostly wait on PostgreSQL, one test at a time in each package binary, and
# httpapi takes most of the shard: the job runs one go test per part at once. A latency shard runs
# its gates as one process with no other test binary of the shard beside them; only after they
# finished ('--' tells the job to wait) does its share of regular tests run in parts.
lines = []
if mine_gates:
    lines.append('^(' + '|'.join(mine_gates) + ')$')
if any(mine) or not mine_gates:
    if mine_gates:
        lines.append('--')
    # Anchors prevent a selected TestFoo from also running TestFooBar on another shard.
    lines.extend('^(' + '|'.join(chosen) + ')$' if chosen else '^$' for chosen in mine)
print('\n'.join(lines))
seconds = max(load[args.shard - 1, part] for part in range(args.parts))
print(f'shard {args.shard}/{args.total}: {len(mine_gates) + sum(map(len, mine))}/{len(names)} names, '
      f'{len(mine_gates)} gates, {sum(map(len, mine))} regular in {args.parts} part(s), '
      f'an estimated {seconds:.0f} s', file=sys.stderr)
