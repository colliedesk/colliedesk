#!/usr/bin/env python3
"""Fails unless the Playwright shards together list every test of the unsharded run exactly once.

Usage: e2e-shard-check.py SHARDS [SPLIT OPTIONS...] -- COMMAND...
COMMAND is the Playwright invocation the shards run (for example
`corepack pnpm exec playwright test --grep-invert @onboarding --fully-parallel`); the check adds
`--list --reporter=json`, asks e2e-shard.py with the SPLIT OPTIONS (--timings FILE ...) for each
shard's file filter, as the e2e job does, and lists COMMAND with that filter.
"""
import collections
import json
import pathlib
import subprocess
import sys

USAGE = 'usage: e2e-shard-check.py SHARDS [SPLIT OPTIONS...] -- COMMAND...'
argv = sys.argv[1:]
if '--' not in argv:
    sys.exit(USAGE)
split, command = argv[:argv.index('--')], argv[argv.index('--') + 1:]
if not split or not split[0].isdigit() or int(split[0]) < 1:
    sys.exit(f'{USAGE}\nshards must be at least 1')
if not command:
    sys.exit(f'{USAGE}\ngive the Playwright command after --')
shards, split = int(split[0]), split[1:]
SPLITTER = pathlib.Path(__file__).with_name('e2e-shard.py')


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


def report(text, what):
    # A package manager may print a banner before the report.
    start = text.find('\n{')
    text = text if text.startswith('{') else text[start + 1:] if start >= 0 else ''
    try:
        parsed = json.loads(text)
    except json.JSONDecodeError as error:
        sys.exit(f'e2e-shard-check: listing {what} is not a JSON report: {error}')
    if parsed.get('errors'):
        sys.exit(f'e2e-shard-check: listing {what} reported errors: {parsed["errors"]}')
    return [key for suite in parsed.get('suites', []) for key in specs(suite, [])]


def listing(extra, what):
    result = subprocess.run(command + ['--list', '--reporter=json'] + extra, capture_output=True, text=True)
    if result.returncode != 0:
        sys.exit(f'e2e-shard-check: listing {what} failed ({result.returncode}):\n{result.stderr}{result.stdout}')
    return result.stdout


unsharded = listing([], 'all tests')
expected = collections.Counter(report(unsharded, 'all tests'))
if not expected:
    sys.exit('e2e-shard-check: the unsharded run lists no tests')
seen = collections.Counter()
for shard in range(1, shards + 1):
    result = subprocess.run([sys.executable, str(SPLITTER), str(shard), str(shards), *split],
                            input=unsharded, capture_output=True, text=True)
    if result.returncode != 0 or not result.stdout.strip():
        sys.exit(f'e2e-shard-check: e2e-shard.py gave shard {shard}/{shards} no filter ({result.returncode}):\n'
                 f'{result.stderr}')
    what = f'shard {shard}/{shards}'
    seen.update(report(listing([result.stdout.strip()], what), what))

problems = [f'missing from every shard: {key}' for key in expected if seen[key] == 0]
problems += [f'runs {count} times across shards: {key}' for key, count in seen.items() if key in expected and count > 1]
problems += [f'in a shard but not in the unsharded run: {key}' for key in seen if key not in expected]
if problems:
    sys.exit('e2e-shard-check: the shards do not run every test exactly once\n' + '\n'.join(problems))
print(f'e2e-shard-check: {shards} shards run all {sum(expected.values())} tests exactly once')
