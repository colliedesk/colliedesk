#!/usr/bin/env python3
"""Agent context budget: AGENTS.md and CLAUDE.md files load into every agent session, so a PR
may not make them larger than its base unless the owner adds the agent-context-increase label."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

CHECK = Path(__file__).resolve().parents[1] / '.github' / 'scripts' / 'agent-context-budget.py'


class AgentContextBudgetTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.git('init', '-qb', 'main')
        self.git('config', 'user.name', 'Test')
        self.git('config', 'user.email', 'test@example.invalid')
        self.write('AGENTS.md', 'a' * 100)
        self.write('CLAUDE.md', 'c' * 50)
        self.write('README.md', 'r' * 10)
        self.base = self.commit('base')

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, text=True).strip()

    def write(self, name, text):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def commit(self, title):
        self.git('add', '-A')
        self.git('commit', '-qm', title)
        return self.git('rev-parse', 'HEAD')

    def check(self, *extra):
        head = self.git('rev-parse', 'HEAD')
        return subprocess.run([sys.executable, str(CHECK), self.base, head, *extra],
                              cwd=self.root, text=True, capture_output=True)

    def test_growth_is_refused_and_names_the_file_and_the_label(self):
        self.write('AGENTS.md', 'a' * 130)
        self.commit('grow')
        result = self.check()
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('AGENTS.md: 100 -> 130 (+30)', result.stdout + result.stderr)
        self.assertIn('agent-context-increase', result.stderr)

    def test_a_new_nested_instruction_file_counts(self):
        self.write('server/AGENTS.md', 'n' * 5)
        self.commit('nested')
        result = self.check()
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn('server/AGENTS.md: 0 -> 5 (+5)', result.stdout + result.stderr)

    def test_growth_passes_with_the_owner_label(self):
        self.write('CLAUDE.md', 'c' * 80)
        self.commit('grow')
        result = self.check('--allow-growth')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('accepted', result.stdout)

    def test_moving_text_between_files_or_shrinking_passes(self):
        self.write('AGENTS.md', 'a' * 120)
        self.write('CLAUDE.md', 'c' * 20)
        self.commit('move')
        self.assertEqual(self.check().returncode, 0)
        self.write('AGENTS.md', 'a' * 10)
        self.commit('cut')
        self.assertEqual(self.check().returncode, 0)

    def test_a_symlink_adds_no_text(self):
        (self.root / 'CLAUDE.md').unlink()
        (self.root / 'CLAUDE.md').symlink_to('AGENTS.md')
        self.base = self.commit('link')
        (self.root / 'server').mkdir()
        (self.root / 'server' / 'CLAUDE.md').symlink_to('../AGENTS.md')
        self.commit('another link')
        result = self.check()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('agent context: 100 -> 100 bytes', result.stdout)

    def test_other_files_do_not_count(self):
        self.write('README.md', 'r' * 5000)
        self.write('docs/agents.md', 'x' * 5000)
        self.commit('docs')
        self.assertEqual(self.check().returncode, 0)


if __name__ == '__main__':
    unittest.main()
