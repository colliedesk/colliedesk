#!/usr/bin/env python3
"""Seconds per Playwright test file from the e2e jobs' logs, as JSON for e2e-shard.py --timings.

Each file is one CI run's log. The shards split a test file's tests between them, so a file's time
in a run is the sum of its tests over the run's shards, retries included; a file takes its median
over the runs: a slow runner slows every test it measures. Names are relative to the Playwright
testDir (web/e2e). Refresh after the suite has grown, from green runs:
  gh run view RUN -R colliedesk/colliedesk --log > run-RUN.log   (once per run)
  python3 .github/scripts/e2e-test-timings.py run-*.log > .github/e2e-test-timings.json
"""
import json
import re
import statistics
import sys

# "job<TAB>step<TAB>" from gh run view, a timestamp, then the list reporter's line for a finished test:
#   ✓    1 [chromium] › e2e/ai-chat.spec.ts:79:1 › asks a question (5.1s)
LINE = re.compile(r'^(?:[^\t]*\t[^\t]*\t)?﻿?(?:\S+Z)?\s+[✓✘]\s+\d+ \[[^\]]+\] › e2e/([^:]+):\d+:\d+ › '
                  r'.* \((\d+(?:\.\d+)?)(ms|s|m)\)$')
UNIT = {'ms': 0.001, 's': 1, 'm': 60}

if len(sys.argv) < 2:
    sys.exit(__doc__)
logs = []
for path in sys.argv[1:]:
    seconds = {}
    with open(path, encoding='utf-8', errors='replace') as log:
        for line in log:
            match = LINE.match(line.rstrip('\r\n'))
            if match:
                seconds[match[1]] = seconds.get(match[1], 0) + float(match[2]) * UNIT[match[3]]
    logs.append(seconds)
names = sorted(set().union(*logs))
if not names:
    sys.exit('e2e-test-timings: no test times in the logs')
timings = {name: round(statistics.median(log[name] for log in logs if name in log), 2) for name in names}
print(json.dumps(timings, indent=1, sort_keys=True))
