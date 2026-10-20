#!/usr/bin/env python3
"""Reject a CI phase older than 120 seconds, or a CD phase older than 60, including queue and setup time."""
import argparse
from datetime import datetime, timezone
import sys

parser = argparse.ArgumentParser(description=__doc__)
BUDGETS = {'ci': 120, 'cd': 60}
parser.add_argument('--phase', choices=sorted(BUDGETS), default='ci')
parser.add_argument('--started-at', required=True)
parser.add_argument('--now', default=None)
args = parser.parse_args()
try:
    start = datetime.fromisoformat(args.started_at.replace('Z', '+00:00'))
    now = datetime.fromisoformat(args.now.replace('Z', '+00:00')) if args.now else datetime.now(timezone.utc)
    if start.tzinfo is None or now.tzinfo is None:
        raise ValueError('timestamps need explicit timezones')
    elapsed = (now - start).total_seconds()
    if elapsed < 0:
        raise ValueError('trigger cannot be in the future')
except ValueError as error:
    parser.error(str(error))
budget = BUDGETS[args.phase]
print(f'{args.phase.upper()} elapsed: {elapsed:.1f}s / {budget}s (from phase trigger)')
sys.exit(0 if elapsed <= budget else 1)
