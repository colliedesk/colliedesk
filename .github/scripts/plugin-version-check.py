#!/usr/bin/env python3
"""A pull request that changes plugins/colliedesk raises the plugin's version.

plugins/colliedesk/.claude-plugin/plugin.json sets "version", which Anthropic's plugin directory asks
for. Claude Code keeps an install from the colliedesk marketplace on that version until it changes,
so a change to the plugin that leaves the version as it was never reaches the people who installed it.

Usage: plugin-version-check.py <base commit>, run in the checkout of the pull request. Exit 1 says what
changed and which version to raise.
"""
import json
import re
import subprocess
import sys

PLUGIN = "plugins/colliedesk"
MANIFEST = PLUGIN + "/.claude-plugin/plugin.json"
SEMVER = re.compile(r"^(\d+)\.(\d+)\.(\d+)$")


def git(*args):
    return subprocess.run(["git", *args], check=True, capture_output=True, text=True).stdout


def version_at(ref):
    try:
        raw = git("show", f"{ref}:{MANIFEST}") if ref else open(MANIFEST, encoding="utf-8").read()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return None
    return json.loads(raw).get("version")


def main():
    if len(sys.argv) != 2:
        sys.exit("usage: plugin-version-check.py <base commit>")
    base = sys.argv[1]
    changed = [line for line in git("diff", "--name-only", base, "--", PLUGIN).splitlines() if line]
    head = version_at(None)
    if head is not None and not SEMVER.match(head):
        sys.exit(f"{MANIFEST}: version {head!r} is not MAJOR.MINOR.PATCH, such as 1.0.0.")
    if not changed:
        print(f"{PLUGIN} unchanged.")
        return
    old = version_at(base)
    if old is None or not SEMVER.match(old):
        print(f"{PLUGIN} changed; the base had no version, so {head} is the first.")
        return
    if head is None or tuple(map(int, SEMVER.match(head).groups())) <= tuple(map(int, SEMVER.match(old).groups())):
        sys.exit(f"{PLUGIN} changed ({', '.join(changed)}) but its version stays {old} (now {head}).\n"
                 f"Raise \"version\" in {MANIFEST}: patch for a fix or a wording change, minor for a new "
                 f"skill or file, major when an install stops working as before. People who installed the "
                 f"plugin from the colliedesk marketplace get the change only when the version goes up.")
    print(f"{PLUGIN} changed and its version goes from {old} to {head}.")


if __name__ == "__main__":
    main()
