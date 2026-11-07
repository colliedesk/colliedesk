#!/usr/bin/env python3
"""A change to plugins/colliedesk must raise its version (.github/scripts/plugin-version-check.py).
Runs the real script in throwaway git repositories."""
import json
import os
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CHECK = os.path.join(ROOT, ".github", "scripts", "plugin-version-check.py")
failures = []


def git(repo, *args):
    subprocess.run(["git", "-C", repo, *args], check=True, capture_output=True)


def write(repo, path, text):
    full = os.path.join(repo, path)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "w", encoding="utf-8") as f:
        f.write(text)


def manifest(version):
    data = {"name": "colliedesk"}
    if version is not None:
        data["version"] = version
    return json.dumps(data)


def case(name, base_version, head_version, change_plugin, want_ok, want="", codex_version="same"):
    with tempfile.TemporaryDirectory() as repo:
        git(repo, "init", "-q")
        git(repo, "config", "user.email", "test@example.com")
        git(repo, "config", "user.name", "test")
        write(repo, "plugins/colliedesk/.claude-plugin/plugin.json", manifest(base_version))
        if codex_version is not None:
            write(repo, "plugins/colliedesk/.codex-plugin/plugin.json", manifest(base_version))
        write(repo, "plugins/colliedesk/skills/a/SKILL.md", "one\n")
        write(repo, "README.md", "one\n")
        git(repo, "add", "-A")
        git(repo, "commit", "-q", "-m", "base")
        write(repo, "plugins/colliedesk/.claude-plugin/plugin.json", manifest(head_version))
        if codex_version is not None:
            write(repo, "plugins/colliedesk/.codex-plugin/plugin.json",
                  manifest(head_version if codex_version == "same" else codex_version))
        write(repo, "plugins/colliedesk/skills/a/SKILL.md" if change_plugin else "README.md", "two\n")
        git(repo, "add", "-A")
        git(repo, "commit", "-q", "-m", "head")
        out = subprocess.run([sys.executable, "-I", CHECK, "HEAD~1"], cwd=repo, capture_output=True, text=True)
        ok = out.returncode == 0
        text = out.stdout + out.stderr
        if ok != want_ok or want not in text:
            failures.append(f"{name}: exit {out.returncode}, output:\n{text}")


case("plugin unchanged", "1.0.0", "1.0.0", False, True, "unchanged")
case("skill changed, version raised", "1.0.0", "1.0.1", True, True, "from 1.0.0 to 1.0.1")
case("skill changed, version kept", "1.0.0", "1.0.0", True, False, "Raise \"version\"")
case("version lowered", "1.2.0", "1.1.9", True, False, "stays 1.2.0")
case("10 is above 9", "1.9.0", "1.10.0", True, True, "to 1.10.0")
case("version removed", "1.0.0", None, True, False, "Raise")
case("first version", None, "1.0.0", True, True, "first")
case("not semver", "1.0.0", "1.0", True, False, "MAJOR.MINOR.PATCH")
case("codex manifest left behind", "1.0.0", "1.1.0", True, False, "Set both to the same version", "1.0.0")
case("no codex manifest", "1.0.0", "1.0.1", True, True, "to 1.0.1", None)

if failures:
    print("\n\n".join(failures))
    sys.exit(1)
print("plugin version check: all cases pass")
