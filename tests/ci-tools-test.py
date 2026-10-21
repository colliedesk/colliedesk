"""Public CLI contracts for CI partitioning and its whole-phase clock."""
import json
import pathlib
import re
import subprocess
import sys
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


def run(script, *args, stdin=''):
    return subprocess.run([sys.executable, str(ROOT / '.github/scripts' / script), *args],
                          input=stdin, text=True, capture_output=True)


class Shards(unittest.TestCase):
    listing = 'TestAlpha\nTestBeta\nFuzzBytes\nExampleClient\nok pkg/one 0.1s\nTestAlpha\nTestGamma\n? pkg/empty [no test files]\n'

    def test_every_test_example_and_fuzz_seed_runs_exactly_once(self):
        patterns = []
        for shard in range(1, 4):
            result = run('go-test-shard.py', str(shard), '3', stdin=self.listing)
            self.assertEqual(result.returncode, 0, result.stderr)
            patterns.append(result.stdout.strip())
        for name in ['TestAlpha', 'TestBeta', 'TestGamma', 'FuzzBytes', 'ExampleClient']:
            self.assertEqual(sum(bool(re.search(p, name)) for p in patterns), 1, name)
        for name in ['TestAlphaExtra', 'PrefixTestBeta']:
            self.assertFalse(any(re.search(p, name) for p in patterns))

    def test_order_and_duplicate_package_names_do_not_change_partition(self):
        first = run('go-test-shard.py', '1', '3', stdin=self.listing)
        second = run('go-test-shard.py', '1', '3', stdin='\n'.join(reversed(self.listing.splitlines())))
        self.assertEqual(first.stdout, second.stdout)
        self.assertEqual(first.returncode, 0, first.stderr)

    def test_large_latency_fixtures_have_a_separate_runner(self):
        listing = self.listing + ('TestInboxListLatencyBudget\nTestPendingAgentRequestsLatencyBudget\n'
                                  'TestStorageUsageStaysFastWithAHugeAttachmentTable\n'
                                  'TestTimelineReadWithHeavyHTMLMailStaysInBudget\n')
        patterns = [run('go-test-shard.py', str(i), '3', stdin=listing).stdout.strip()
                    for i in range(1, 4)]
        for name in ['TestInboxListLatencyBudget', 'TestPendingAgentRequestsLatencyBudget',
                     'TestStorageUsageStaysFastWithAHugeAttachmentTable',
                     'TestTimelineReadWithHeavyHTMLMailStaysInBudget']:
            self.assertFalse(any(re.search(p, name) for p in patterns[:-1]))
            self.assertRegex(name, patterns[-1])
        self.assertNotRegex('TestAlpha', patterns[-1])

    def test_parts_split_a_regular_shard_and_never_the_latency_one(self):
        listing = self.listing + 'TestDelta\nTestEpsilon\nTestZeta\nTestInboxListLatencyBudget\nTestTrialCapStaysFast\n'
        whole = run('go-test-shard.py', '1', '2', stdin=listing).stdout.strip()
        parted = run('go-test-shard.py', '1', '2', '--parts', '2', stdin=listing)
        self.assertEqual(parted.returncode, 0, parted.stderr)
        parts = parted.stdout.split()
        self.assertEqual(len(parts), 2)
        for name in ['TestAlpha', 'TestBeta', 'TestGamma', 'TestDelta', 'TestEpsilon', 'TestZeta',
                     'FuzzBytes', 'ExampleClient']:
            self.assertEqual(sum(bool(re.search(p, name)) for p in parts), int(bool(re.search(whole, name))), name)
        latency = run('go-test-shard.py', '2', '2', '--parts', '2', stdin=listing).stdout.split()
        self.assertEqual(latency, [run('go-test-shard.py', '2', '2', stdin=listing).stdout.strip()])
        self.assertEqual(run('go-test-shard.py', '1', '2', '--parts', '1', stdin=listing).stdout.strip(), whole)
        self.assertNotEqual(run('go-test-shard.py', '1', '2', '--parts', '0', stdin=listing).returncode, 0)

    def test_empty_inventory_and_invalid_shards_fail_closed(self):
        for args, listing in [(('0', '3'), self.listing), (('4', '3'), self.listing),
                              (('1', '0'), self.listing), (('1', '3'), '? pkg [no test files]')]:
            self.assertNotEqual(run('go-test-shard.py', *args, stdin=listing).returncode, 0)

    def test_empty_shard_matches_nothing(self):
        result = run('go-test-shard.py', '3', '3', stdin='TestOnly\n')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIsNone(re.search(result.stdout.strip(), 'TestOnly'))


class GoRelease(unittest.TestCase):
    """go-release.py names the Go release setup-go would install for a minor line, so the toolchain
    cache is keyed on it and setup-go is told that exact version."""
    manifest = json.dumps([
        {'version': '1.28.0-rc1', 'stable': False, 'files': [{'platform': 'linux', 'arch': 'x64'}]},
        {'version': '1.27.10', 'stable': True, 'files': [{'platform': 'darwin', 'arch': 'arm64'}]},
        {'version': '1.27.3', 'stable': True, 'files': [{'platform': 'linux', 'arch': 'x64'}]},
        {'version': '1.27.20', 'stable': False, 'files': [{'platform': 'linux', 'arch': 'x64'}]},
        {'version': '1.27.2', 'stable': True, 'files': [{'platform': 'linux', 'arch': 'x64'}]},
        {'version': '1.270.1', 'stable': True, 'files': [{'platform': 'linux', 'arch': 'x64'}]},
    ])

    def test_newest_stable_linux_release_of_the_minor_line(self):
        result = run('go-release.py', '1.27', stdin=self.manifest)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, 'version=1.27.3\n')

    def test_patch_numbers_compare_as_numbers(self):
        manifest = json.dumps([{'version': v, 'stable': True, 'files': [{'platform': 'linux', 'arch': 'x64'}]}
                               for v in ['1.27.9', '1.27.10', '1.27.0']])
        self.assertEqual(run('go-release.py', '1.27', stdin=manifest).stdout, 'version=1.27.10\n')

    def test_no_release_or_a_broken_manifest_fails_closed(self):
        for line, manifest in [('1.29', self.manifest), ('1.27', 'not json'), ('1.27', ''), ('1.27.x', self.manifest)]:
            result = run('go-release.py', line, stdin=manifest)
            self.assertNotEqual(result.returncode, 0, line)
            self.assertEqual(result.stdout, '')


class Onboarding(unittest.TestCase):
    def test_title_filters_partition_every_onboarding_journey(self):
        workflow = (ROOT / '.github/workflows/ci.yml').read_text()
        patterns = re.findall(r"pattern: '([^']+)'", workflow)
        self.assertEqual(len(patterns), 2)
        for title, expected in [
            ('e2e/import.spec.ts freshdesk import: reconcile counts @onboarding', [True, False]),
            ('e2e/signup.spec.ts cloud signup @onboarding', [False, True]),
            ('future journey @onboarding', [False, True]),
            ('freshdesk import: future journey @onboarding', [True, False]),
            ('ordinary browser test', [False, False]),
        ]:
            self.assertEqual([bool(re.search(p, title)) for p in patterns], expected, title)


class Budget(unittest.TestCase):
    def test_queue_time_counts_toward_the_120_second_limit(self):
        for now, expected in [('2026-10-19T13:01:59Z', 0), ('2026-10-19T13:02:00Z', 0),
                              ('2026-10-19T13:02:01Z', 1)]:
            result = run('ci-budget.py', '--started-at', '2026-10-19T13:00:00Z', '--now', now)
            self.assertEqual(result.returncode, expected, result.stderr)

    def test_cd_phase_has_a_60_second_limit(self):
        for now, expected in [('2026-10-19T13:01:00Z', 0), ('2026-10-19T13:01:01Z', 1)]:
            result = run('ci-budget.py', '--phase', 'cd', '--started-at', '2026-10-19T13:00:00Z', '--now', now)
            self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        self.assertNotEqual(run('ci-budget.py', '--phase', 'qa', '--started-at', '2026-10-19T13:00:00Z').returncode, 0)

    def test_missing_timezone_and_future_start_fail_closed(self):
        for start in ['bad', '2026-10-19T13:00:00', '2026-10-19T14:00:00Z']:
            self.assertNotEqual(run('ci-budget.py', '--started-at', start,
                                    '--now', '2026-10-19T13:00:00Z').returncode, 0)


# GitHub Free runs at most 20 GitHub-hosted jobs at once for the whole account, across every repo.
# A pull request's jobs that start together must stay under it, with a slot left for another
# repository's job, or the last ones queue and that wait counts toward the 120-second CI clock.
RUNNER_LIMIT = 20
RUNNER_SLACK = 1


def workflow_jobs(path):
    """Yields (name, lines) for each top-level job of a workflow file."""
    name, body, in_jobs = None, [], False
    for line in path.read_text().splitlines():
        if line == 'jobs:':
            in_jobs = True
        elif in_jobs and re.match(r'^  [A-Za-z0-9_-]+:\s*$', line):
            if name:
                yield name, body
            name, body = line.strip()[:-1], []
        elif in_jobs and name:
            body.append(line)
    if name:
        yield name, body


def runners(path):
    """How many runners a workflow's jobs without needs ask for at the same moment."""
    total = 0
    for _, body in workflow_jobs(path):
        text = '\n'.join(body)
        if re.search(r'^    needs:', text, re.M):
            continue
        local = re.search(r'^    uses: \./(\S+)', text, re.M)
        if local:
            total += runners(ROOT / local.group(1))
            continue
        matrix = re.search(r'^      matrix:\n((?:        .*\n?)+)', text, re.M)
        if not matrix:
            total += 1
            continue
        inline = re.search(r'^        [a-z]+: \[([^]]*)\]', matrix.group(1), re.M)
        include = re.findall(r'^          - ', matrix.group(1), re.M)
        total += len(inline.group(1).split(',')) if inline else len(include)
    return total


class RunnerConcurrency(unittest.TestCase):
    def test_pull_request_jobs_fit_the_account_runner_limit(self):
        workflows = [path for path in sorted((ROOT / '.github/workflows').glob('*.yml'))
                     if re.search(r'^  pull_request:', path.read_text(), re.M)]
        counts = {path.name: runners(path) for path in workflows}
        self.assertLessEqual(sum(counts.values()), RUNNER_LIMIT - RUNNER_SLACK, counts)

    def test_matrix_sizes_are_counted(self):
        self.assertEqual(runners(ROOT / '.github/workflows/codeql.yml'), 3)


class PublishEdge(unittest.TestCase):
    """edge is what Cloud and self-hosters pull: it moves only for a passing CI run of a push to this
    repository's main, only to that run's commit, and only while that commit is still main's head."""

    def test_edge_moves_only_after_ci_passed_on_main(self):
        workflow = (ROOT / '.github/workflows/publish-edge.yml').read_text()
        self.assertRegex(workflow, r'workflow_run:\n\s+workflows: \[CI\]\n\s+types: \[completed\]\n\s+branches: \[main\]')
        for guard in ["github.event.workflow_run.conclusion == 'success'",
                      "github.event.workflow_run.event == 'push'",
                      'github.event.workflow_run.head_repository.full_name == github.repository']:
            self.assertIn(guard, workflow)
        self.assertIn('ci-budget.py --phase cd', workflow)
        ci = (ROOT / '.github/workflows/ci.yml').read_text()
        self.assertIsNone(re.search(r'^  publish-edge:', ci, re.M), 'ci.yml still defines publish-edge')

class PackageManager(unittest.TestCase):
    def test_workflows_run_pnpm_through_corepack_only(self):
        # corepack reads packageManager; pnpm/action-setup installed a second pnpm from npm on
        # every job and once hung 84 seconds on a registry reset.
        for path in sorted((ROOT / '.github/workflows').glob('*.yml')):
            text = path.read_text()
            self.assertFalse('pnpm/action-setup' in text, f'{path.name} uses pnpm/action-setup')
            for line in text.splitlines():
                for match in re.finditer(r'(\S*)\s+pnpm\s', ' ' + line + ' '):
                    if match.group(1) in ('run:', '&&', '||', ';', ''):
                        self.fail(f'{path.name}: pnpm without corepack: {line.strip()}')


if __name__ == '__main__':
    unittest.main()
