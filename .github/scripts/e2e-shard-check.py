#!/usr/bin/env python3
"""Fails unless the Playwright shards together list every test of the unsharded run exactly once.

Usage: e2e-shard-check.py SHARDS -- COMMAND...
COMMAND is the Playwright invocation the shards run (for example
`corepack pnpm exec playwright test --grep-invert @onboarding --fully-parallel`); the check adds
`--list --reporter=json`, then `--shard=i/SHARDS` for each shard.
"""
import argparse
import collections
import json
import subprocess
import sys

parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
parser.add_argument('shards', type=int)
parser.add_argument('command', nargs=argparse.REMAINDER)
args = parser.parse_args()
command = args.command[1:] if args.command[:1] == ['--'] else args.command
if args.shards < 1:
    parser.error('shards must be at least 1')
if not command:
    parser.error('give the Playwright command after --')


def specs(suite, path):
    """Yields one key per test: file, project and title path. Playwright refuses two tests with one
    title path in a file. The line is left out: in run 39904337642 the unsharded listing put a test at
    line 7 of its file and the shard's listing at line 5."""
    path = path + [suite['title']] if suite.get('title') else path
    for spec in suite.get('specs', []):
        for test in spec.get('tests', []):
            name = ' › '.join(path + [spec['title']])
            yield f"{spec['file']} [{test.get('projectName', '')}] {name}"
    for child in suite.get('suites', []):
        yield from specs(child, path)


def listing(extra):
    result = subprocess.run(command + ['--list', '--reporter=json'] + extra, capture_output=True, text=True)
    if result.returncode != 0:
        sys.exit(f'e2e-shard-check: listing {extra or "all tests"} failed ({result.returncode}):\n'
                 f'{result.stderr}{result.stdout}')
    # A package manager may print a banner before the report.
    start = result.stdout.find('\n{')
    text = result.stdout if result.stdout.startswith('{') else result.stdout[start + 1:] if start >= 0 else ''
    try:
        report = json.loads(text)
    except json.JSONDecodeError as error:
        sys.exit(f'e2e-shard-check: listing {extra or "all tests"} is not a JSON report: {error}')
    if report.get('errors'):
        sys.exit(f'e2e-shard-check: listing {extra or "all tests"} reported errors: {report["errors"]}')
    return [key for suite in report.get('suites', []) for key in specs(suite, [])]


expected = collections.Counter(listing([]))
if not expected:
    sys.exit('e2e-shard-check: the unsharded run lists no tests')
seen = collections.Counter()
for shard in range(1, args.shards + 1):
    seen.update(listing([f'--shard={shard}/{args.shards}']))

problems = [f'missing from every shard: {key}' for key in expected if seen[key] == 0]
problems += [f'runs {count} times across shards: {key}' for key, count in seen.items() if key in expected and count > 1]
problems += [f'in a shard but not in the unsharded run: {key}' for key in seen if key not in expected]
if problems:
    sys.exit('e2e-shard-check: the shards do not run every test exactly once\n' + '\n'.join(problems))
print(f'e2e-shard-check: {args.shards} shards run all {sum(expected.values())} tests exactly once')
