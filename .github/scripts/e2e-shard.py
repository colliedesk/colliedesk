#!/usr/bin/env python3
"""Give one Playwright shard whole test files, balanced by measured time, as one file filter.

Usage: playwright test ARGS --list --reporter=json | e2e-shard.py SHARD SHARDS --timings FILE
Prints a regular expression in slashes, for example /(^|\\/)(a\\.spec\\.ts|b\\.spec\\.ts)$/, which
`playwright test ARGS FILTER` matches against each test file's path. Files go longest first to the
shard that is predicted to finish first; a file the timings lack weighs the median of the listed
files they have. e2e-test-timings.py writes the timings file from the e2e jobs' logs.
"""
import argparse
import json
import re
import statistics
import sys

parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
parser.add_argument('shard', type=int)
parser.add_argument('shards', type=int)
parser.add_argument('--timings', required=True, help='JSON object of seconds per test file, relative to testDir')
parser.add_argument('--parallel', type=float, default=1,
                    help="how many of a shard's tests run at once (its workers), so a shard takes its summed "
                         'test time divided by this')
parser.add_argument('--last-shard-delay', type=float, default=0,
                    help='seconds after the other shards that the last shard starts its tests')
args = parser.parse_args()
if not 1 <= args.shard <= args.shards:
    parser.error('shard must be between 1 and shards')
if args.parallel <= 0 or args.last_shard_delay < 0:
    parser.error('parallel must be positive and the delay non-negative')
try:
    with open(args.timings) as file:
        timings = json.load(file)
except (OSError, ValueError) as error:
    parser.error(f'timings: {error}')
if not isinstance(timings, dict) or not all(
        isinstance(seconds, (int, float)) and not isinstance(seconds, bool) and seconds >= 0
        for seconds in timings.values()):
    parser.error('timings must map file names to non-negative seconds')


def files(suite):
    for spec in suite.get('specs', []):
        if spec.get('tests'):
            yield spec['file']
    for child in suite.get('suites', []):
        yield from files(child)


text = sys.stdin.read()
# A package manager may print a banner before the report.
start = text.find('\n{')
text = text if text.startswith('{') else text[start + 1:] if start >= 0 else ''
try:
    report = json.loads(text)
except json.JSONDecodeError as error:
    sys.exit(f'e2e-shard: the listing is not a JSON report: {error}')
if report.get('errors'):
    sys.exit(f'e2e-shard: the listing reported errors: {report["errors"]}')
listed = sorted({name for suite in report.get('suites', []) for name in files(suite)})
if not listed:
    sys.exit('e2e-shard: the listing has no tests')
if any(':' in name for name in listed):
    # Playwright reads a colon in a file filter as a line number.
    sys.exit(f'e2e-shard: a file name has a colon: {[n for n in listed if ":" in n]}')
measured = [timings[name] for name in listed if name in timings]
if not measured:
    sys.exit('e2e-shard: the timings name none of the listed files; refresh them with e2e-test-timings.py')
fallback = statistics.median(measured)
weight = {name: timings.get(name, fallback) for name in listed}

offsets = [0.0] * (args.shards - 1) + [args.last_shard_delay]
loads = [0.0] * args.shards
assigned = [[] for _ in range(args.shards)]
for name in sorted(listed, key=lambda n: (-weight[n], n)):
    shard = min(range(args.shards), key=lambda i: (offsets[i] + loads[i] / args.parallel, i))
    loads[shard] += weight[name]
    assigned[shard].append(name)

print(f'e2e-shard: {len(measured)} of {len(listed)} files measured', file=sys.stderr)
for i, names in enumerate(assigned):
    print(f'e2e-shard: shard {i + 1}/{args.shards}: {len(names)} files, '
          f'{offsets[i] + loads[i] / args.parallel:.0f}s', file=sys.stderr)
if not all(assigned):
    # An empty filter would run every test.
    sys.exit('e2e-shard: a shard has no files; use fewer shards')
# Escapes only what JavaScript reads as syntax: Playwright builds the RegExp without the u flag.
escape = lambda name: re.sub(r'([.^$*+?()[\]{}|\\/])', r'\\\1', name)
print('/(^|\\/)(' + '|'.join(escape(n) for n in sorted(assigned[args.shard - 1])) + ')$/')
