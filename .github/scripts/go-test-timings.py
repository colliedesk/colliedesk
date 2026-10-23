#!/usr/bin/env python3
"""Seconds per top-level Go test from go test -v logs, as JSON for go-test-shard.py --timings.

Each file is one CI run's log (or a local go test -v output); a name's times in one run are summed
over its packages, and a name takes its median over the runs: a slow runner, or a run beside heavier
tests, slows every test it measures. Refresh after the suite has grown, from green runs:
  gh run view RUN -R colliedesk/colliedesk --log > run.log   (once per run)
  python3 .github/scripts/go-test-timings.py run*.log > .github/go-test-timings.json
"""
import json
import re
import statistics
import sys

# A job log line is "job<TAB>step<TAB>timestamp text"; subtests are indented, so they never match.
LINE = re.compile(r'^(?:[^\t]*\t[^\t]*\t\ufeff?\S+Z )?--- (?:PASS|FAIL|SKIP): ((?:Test|Example|Fuzz)\w*) \((\d+(?:\.\d+)?)s\)$')

if len(sys.argv) < 2:
    sys.exit(__doc__)
runs = []
for path in sys.argv[1:]:
    seconds = {}
    with open(path, encoding='utf-8', errors='replace') as log:
        for line in log:
            match = LINE.match(line.rstrip('\r\n'))
            if match:
                seconds[match[1]] = seconds.get(match[1], 0) + float(match[2])
    runs.append(seconds)
names = sorted(set().union(*runs))
if not names:
    sys.exit('go-test-timings: no test times found; run go test with -v')
timings = {}
for name in names:
    measured = [seconds[name] for seconds in runs if name in seconds]
    timings[name] = round(statistics.median(measured), 2)
print(json.dumps(timings, indent=1, sort_keys=True))
