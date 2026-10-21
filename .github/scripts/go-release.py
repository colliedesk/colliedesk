#!/usr/bin/env python3
"""Print version=<newest stable linux/x64 Go release of a minor line> from setup-go's manifest on stdin."""
import argparse
import json
import re
import sys

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('line', help='a minor line such as 1.27')
args = parser.parse_args()
if not re.fullmatch(r'\d+\.\d+', args.line):
    parser.error('line must look like 1.27')
try:
    manifest = json.load(sys.stdin)
except ValueError as error:
    parser.error(f'unreadable manifest: {error}')
patches = [int(release['version'].rsplit('.', 1)[1]) for release in manifest
           if release.get('stable') and re.fullmatch(re.escape(args.line) + r'\.\d+', release['version'])
           and any(f.get('platform') == 'linux' and f.get('arch') == 'x64' for f in release.get('files', []))]
if not patches:
    parser.error(f'no stable linux/x64 release of Go {args.line} in the manifest')
print(f'version={args.line}.{max(patches)}')
