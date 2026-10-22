#!/usr/bin/env python3
"""Print reuse=true when a push's tree already passed a full pull request CI run, else reuse=false.

A merge commit whose second parent (the pull request head) already contained main has that head's
tree, and a pull_request run tests the merge of the head into main, which is then the same tree. If
such a run from this repository passed, every test already ran on these exact files. Any other
push, and any API error, prints reuse=false so the full suite runs.
"""
import argparse
import json
import os
import shlex
import subprocess
import sys

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--repo', required=True)
parser.add_argument('--sha', required=True)
args = parser.parse_args()
gh = shlex.split(os.environ.get('PR_VERIFIED_GH', 'gh'))


def api(path):
    result = subprocess.run([*gh, 'api', path], capture_output=True, text=True, check=True)
    return json.loads(result.stdout)


def passed_run():
    commit = api(f'repos/{args.repo}/git/commits/{args.sha}')
    if len(commit['parents']) != 2:
        return None, 'not a merge commit'
    head = commit['parents'][1]['sha']
    if api(f'repos/{args.repo}/git/commits/{head}')['tree']['sha'] != commit['tree']['sha']:
        return None, f'tree differs from pull request head {head}: it was behind main'
    runs = api(f'repos/{args.repo}/actions/workflows/ci.yml/runs?event=pull_request&status=success&head_sha={head}')
    for run in runs['workflow_runs']:
        if run['conclusion'] == 'success' and run['head_repository']['full_name'] == args.repo:
            return run['html_url'], f'pull request head {head} has the same tree'
    return None, f'no passed pull request run from {args.repo} for {head}'


try:
    url, reason = passed_run()
except (subprocess.CalledProcessError, OSError, ValueError, KeyError, IndexError, TypeError) as error:
    url, reason = None, f'cannot decide ({error})'
print(f'reuse={"true" if url else "false"}')
print(f'{reason}; ' + (f'every test passed on these files in {url}' if url else 'running every job'), file=sys.stderr)
