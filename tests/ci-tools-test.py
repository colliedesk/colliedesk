"""Public CLI contracts for CI partitioning and its whole-phase clock."""
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
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

    def test_latency_fixtures_split_over_their_own_runners_with_the_kb_seed_on_one(self):
        latency = ['TestHelpCenterPagesLatencyBudget', 'TestKnowledgeBaseSearchLatencyBudget',
                   'TestKBMediaLibraryLatencyBudget', 'TestInboxListLatencyBudget',
                   'TestStorageUsageStaysFastWithAHugeAttachmentTable', 'TestTrialCapStaysFast']
        listing = self.listing + 'TestDelta\n' + '\n'.join(latency) + '\n'
        shards = []
        for shard in range(1, 5):
            result = run('go-test-shard.py', str(shard), '4', '--latency-shards', '2', '--parts', '3', stdin=listing)
            self.assertEqual(result.returncode, 0, result.stderr)
            shards.append(result.stdout.split())
        for name in ['TestAlpha', 'TestBeta', 'TestGamma', 'TestDelta', 'FuzzBytes', 'ExampleClient'] + latency:
            hits = [i for i, patterns in enumerate(shards) if any(re.search(p, name) for p in patterns)]
            self.assertEqual(len(hits), 1, name)
            self.assertEqual(hits[0] >= 2, name in latency, name)
        # One process on each latency runner, so a gate never measures beside another test binary
        # of its own shard; the tests that build the shared knowledge-base seed stay together.
        self.assertEqual([len(patterns) for patterns in shards[2:]], [1, 1])
        for name in latency[:3]:
            self.assertRegex(name, shards[2][0])
        for name in latency[3:]:
            self.assertRegex(name, shards[3][0])

    def test_latency_runners_take_a_share_of_regular_tests_only_after_their_gates(self):
        latency = ['TestHelpCenterPagesLatencyBudget', 'TestInboxListLatencyBudget', 'TestTrialCapStaysFast']
        regular = [f'TestRegular{i:03d}' for i in range(140)] + ['FuzzBytes', 'ExampleClient']
        listing = '\n'.join(regular + latency) + '\n'
        shards = []
        for shard in range(1, 9):
            result = run('go-test-shard.py', str(shard), '8', '--latency-shards', '2', '--parts', '3',
                         '--latency-share', '0.5', stdin=listing)
            self.assertEqual(result.returncode, 0, result.stderr)
            shards.append(result.stdout.split('\n')[:-1])
        for name in regular + latency:
            hits = [i for i, lines in enumerate(shards) for p in lines if p != '--' and re.search(p, name)]
            self.assertEqual(len(hits), 1, name)
        for lines in shards[6:]:
            # The gates run alone; the regular slice starts once they finished (the '--' line).
            self.assertEqual(lines[1], '--', lines)
            self.assertFalse(any(re.search(lines[0], name) for name in regular))
            self.assertEqual(len(lines[2:]), 3)
        self.assertRegex('TestHelpCenterPagesLatencyBudget', shards[6][0])
        for name in latency[1:]:
            self.assertRegex(name, shards[7][0])
        # Each latency runner takes about half of what a regular runner takes.
        count = lambda lines: sum(bool(re.search(p, name)) for p in lines if p != '--' for name in regular)
        regular_counts = [count(lines) for lines in shards[:6]]
        latency_counts = [count(lines[2:]) for lines in shards[6:]]
        self.assertLessEqual(max(regular_counts) - min(regular_counts), 1, regular_counts)
        for share in latency_counts:
            self.assertAlmostEqual(share, min(regular_counts) / 2, delta=1, msg=(regular_counts, latency_counts))
        self.assertTrue(all('--' not in lines for lines in shards[:6]))

    def test_each_latency_runner_can_take_its_own_share(self):
        latency = ['TestHelpCenterPagesLatencyBudget', 'TestInboxListLatencyBudget']
        regular = [f'TestRegular{i:03d}' for i in range(130)]
        listing = '\n'.join(regular + latency) + '\n'
        shards = [run('go-test-shard.py', str(shard), '8', '--latency-shards', '2', '--parts', '3',
                      '--latency-share', '0.5,0', stdin=listing) for shard in range(1, 9)]
        for result in shards:
            self.assertEqual(result.returncode, 0, result.stderr)
        lines = [result.stdout.split('\n')[:-1] for result in shards]
        for name in regular + latency:
            self.assertEqual(sum(bool(re.search(p, name)) for shard in lines for p in shard if p != '--'), 1, name)
        # The second latency runner's gates take about as long as a regular shard: gates only.
        self.assertEqual(len(lines[7]), 1)
        self.assertRegex('TestInboxListLatencyBudget', lines[7][0])
        count = lambda shard: sum(bool(re.search(p, name)) for p in shard if p != '--' for name in regular)
        self.assertAlmostEqual(count(lines[6][2:]), count(lines[0]) / 2, delta=1)
        for share in ('0.5,0,1', '0.5,x'):
            self.assertNotEqual(run('go-test-shard.py', '1', '8', '--latency-shards', '2',
                                    '--latency-share', share, stdin=listing).returncode, 0, share)

    def test_the_go_job_starts_a_latency_shards_regular_tests_only_after_its_gates_exit(self):
        go = '\n'.join(dict(workflow_jobs(ROOT / '.github/workflows/ci.yml'))['go'])
        script = re.search(r'- name: Test\n(?:        .*\n)*?        run: \|\n((?:          .*\n?)+)', go).group(1)
        script = '\n'.join(line[10:] for line in script.splitlines())
        with tempfile.TemporaryDirectory() as tmp:
            tmp = pathlib.Path(tmp)
            # A stand-in go: the gate sleeps, a regular pattern fails, each logs start and end.
            (tmp / 'go').write_text('#!/bin/bash\necho "start $4" >> "$LOG"\n'
                                    '[ "$4" = GATE ] && sleep 0.5\necho "end $4" >> "$LOG"\n'
                                    '[ "$4" = B ] && exit 3\nexit 0\n')
            (tmp / 'go').chmod(0o755)
            runner = tmp / 'temp'
            runner.mkdir()
            (runner / 'go-test-patterns.txt').write_text('GATE\n--\nA\nB\n')
            env = dict(os.environ, PATH=f'{tmp}:{os.environ["PATH"]}', RUNNER_TEMP=str(runner), LOG=str(tmp / 'log'))
            result = subprocess.run(['bash', '-e', '-c', script], env=env, capture_output=True, text=True)
            log = (tmp / 'log').read_text().split('\n')
        self.assertNotEqual(result.returncode, 0, 'a failing pattern after the gates fails the step')
        self.assertLess(log.index('end GATE'), log.index('start A'), log)
        self.assertLess(log.index('end GATE'), log.index('start B'), log)
        self.assertNotIn('start --', log)

    def test_latency_share_defaults_to_none_and_rejects_negative(self):
        listing = self.listing + 'TestInboxListLatencyBudget\n'
        plain = run('go-test-shard.py', '3', '3', stdin=listing)
        zero = run('go-test-shard.py', '3', '3', '--latency-share', '0', stdin=listing)
        self.assertEqual(plain.stdout, zero.stdout)
        self.assertEqual(len(plain.stdout.split()), 1)
        self.assertNotEqual(run('go-test-shard.py', '3', '3', '--latency-share', '-1', stdin=listing).returncode, 0)

    def test_latency_shards_leave_at_least_one_regular_shard(self):
        for args in (('1', '2', '--latency-shards', '2'), ('1', '3', '--latency-shards', '0')):
            self.assertNotEqual(run('go-test-shard.py', *args, stdin=self.listing).returncode, 0, args)

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


FAKE_GH = '''import json, os, sys
responses = json.loads(os.environ['FAKE_GH_RESPONSES'])
path = sys.argv[-1]
if sys.argv[1] != 'api' or path not in responses:
    sys.exit('fake gh: no response for ' + ' '.join(sys.argv[1:]))
print(json.dumps(responses[path]))
'''


class PrVerified(unittest.TestCase):
    """A push to main whose tree a same-repository pull request run already passed in full is not
    tested again; any other push, and any doubt, runs every job."""
    repo, merge, head, base = 'colliedesk/colliedesk', 'm' * 40, 'h' * 40, 'b' * 40
    runs = f'repos/{repo}/actions/workflows/ci.yml/runs?event=pull_request&status=success&head_sha={head}'

    def decide(self, responses):
        with tempfile.TemporaryDirectory() as tmp:
            fake = pathlib.Path(tmp) / 'gh.py'
            fake.write_text(FAKE_GH)
            env = {**os.environ, 'PR_VERIFIED_GH': f'{sys.executable} {fake}',
                   'FAKE_GH_RESPONSES': json.dumps(responses)}
            return subprocess.run([sys.executable, str(ROOT / '.github/scripts/pr-verified.py'),
                                   '--repo', self.repo, '--sha', self.merge],
                                  text=True, capture_output=True, env=env)

    def responses(self, head_tree='T', run_repo=repo, runs=1):
        return {
            f'repos/{self.repo}/git/commits/{self.merge}': {
                'tree': {'sha': 'T'}, 'parents': [{'sha': self.base}, {'sha': self.head}]},
            f'repos/{self.repo}/git/commits/{self.head}': {'tree': {'sha': head_tree}, 'parents': [{'sha': self.base}]},
            self.runs: {'workflow_runs': [{'html_url': 'https://github.com/run/1', 'conclusion': 'success',
                                           'head_repository': {'full_name': run_repo}}] * runs},
        }

    def test_merge_of_a_passed_up_to_date_pull_request_is_not_tested_again(self):
        result = self.decide(self.responses())
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, 'reuse=true\n')
        self.assertIn('https://github.com/run/1', result.stderr)

    def test_anything_else_runs_every_job(self):
        single_parent = self.responses()
        single_parent[f'repos/{self.repo}/git/commits/{self.merge}']['parents'] = [{'sha': self.base}]
        for name, responses in [('tree differs: the branch was behind main', self.responses(head_tree='U')),
                                ('no passed pull request run', self.responses(runs=0)),
                                ('the passed run came from a fork', self.responses(run_repo='someone/colliedesk')),
                                ('a direct push, not a merge', single_parent),
                                ('the API did not answer', {})]:
            result = self.decide(responses)
            self.assertEqual(result.returncode, 0, name + ': ' + result.stderr)
            self.assertEqual(result.stdout, 'reuse=false\n', name)


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
    """How many runners a pull request's jobs ask for at the same moment: jobs without needs, and
    jobs that need only pr-verified, which pull requests skip at once."""
    total = 0
    for _, body in workflow_jobs(path):
        text = '\n'.join(body)
        needs = re.search(r'^    needs:\s*(.*)$', text, re.M)
        if needs and needs.group(1).strip() != 'pr-verified':
            continue
        if re.search(r"^    if: github.event_name == 'push'\s*$", text, re.M):
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

    def test_jobs_behind_pr_verified_count_and_push_only_jobs_do_not(self):
        # 8 Go shards, web, e2e, 2 onboarding, image, osv-scan, roadmap, 3 CodeQL; not pr-verified.
        self.assertEqual(runners(ROOT / '.github/workflows/ci.yml'), 18)


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

class GoBuildCache(unittest.TestCase):
    """actions/cache tries every key on the pull request's own ref before main's. CodeQL's setup-go
    saves a build-only setup-go-* entry on that ref, so a restore key matching it shadowed main's
    entry with the test builds, and every Go shard compiled the tests again (24 to 42 seconds).
    Main saved only from a Go shard, which a merge pr-verified passes skips, so a go.sum change (the
    Anthropic SDK: 17 modules, about 23 seconds of compiling per job) never reached the cache."""

    def saves(self):
        found = []
        for path in sorted((ROOT / '.github/workflows').glob('*.yml')):
            for name, body in workflow_jobs(path):
                text = '\n'.join(body) + '\n'
                for step in re.findall(r'^      - (?:(?!\n      - ).)*?uses: actions/cache/save@.*?(?=\n      - |\Z)', text, re.S | re.M):
                    found.append((path.name, name, step))
        return found

    def test_go_jobs_restore_only_entries_the_go_cache_saves(self):
        saves = self.saves()
        self.assertTrue(saves)
        prefixes = {re.search(r'key: (.+?)\$\{\{ github\.sha \}\}', step).group(1) for _, _, step in saves}
        self.assertEqual(len(prefixes), 1, prefixes)
        prefix = prefixes.pop().split('${{')[0]
        workflow = (ROOT / '.github/workflows/ci.yml').read_text()
        restores = re.findall(r'restore-keys: \|\n((?:\s{12}\S.*\n)+)', workflow)
        self.assertEqual(len(restores), 3, 'the go, e2e and onboarding jobs restore the Go cache')
        for block in restores:
            for key in (line.strip() for line in block.splitlines()):
                self.assertTrue(key.startswith(prefix), f'{key} can match an entry setup-go saved')

    def test_every_push_to_main_saves_the_test_builds_even_when_ci_skips_the_go_jobs(self):
        workflow = (ROOT / '.github/workflows/go-cache.yml').read_text()
        self.assertRegex(workflow, r'on:\n  push:\n    branches: \[main\]\n')
        self.assertNotIn('pull_request', workflow)
        self.assertNotIn('needs:', workflow)
        self.assertRegex(workflow, r'go test -c -o \S+ \./\.\.\.')
        self.assertEqual([job for path, job, _ in self.saves() if path == 'go-cache.yml'], ['go-cache'])

    def test_a_pull_request_saves_once_for_a_go_sum_main_has_not_cached(self):
        ci = [(job, step) for path, job, step in self.saves() if path == 'ci.yml']
        self.assertEqual(len(ci), 1, ci)
        job, step = ci[0]
        self.assertEqual(job, 'go')
        condition = ' '.join(line.strip() for line in
                             re.search(r'if: >-\n((?:          .*\n)+)', step).group(1).splitlines())
        self.assertIn("github.event_name == 'pull_request'", condition)
        # A latency runner: it ends 30 to 60 seconds before the regular shards, so the upload is free.
        self.assertIn('matrix.shard == 7', condition)
        self.assertIn("!startsWith(steps.go-cache.outputs.cache-matched-key, format('go-{0}-{1}-{2}-', "
                      "runner.os, steps.go.outputs.go-version, hashFiles('server/go.sum')))", condition)
        go = '\n'.join(dict(workflow_jobs(ROOT / '.github/workflows/ci.yml'))['go'])
        self.assertIn('id: go-cache', go)
        self.assertRegex(go, r"latency-shards 2\b")
        self.assertIn("SHARDS: ${{ strategy.job-total }}", go)
        self.assertRegex(go, r'shard: \[1, 2, 3, 4, 5, 6, 7, 8\]')

    def test_setup_go_saves_no_cache_of_its_own_outside_codeql(self):
        # setup-go saves its own cache unless told not to: only CodeQL, whose setup-go- entries the Go
        # jobs never restore, may leave it on.
        for path in sorted((ROOT / '.github/workflows').glob('*.yml')):
            for name, body in workflow_jobs(path):
                text = '\n'.join(body)
                for step in re.findall(r'uses: actions/setup-go@\S+.*?(?=\n      - |\Z)', text, re.S):
                    if path.name != 'codeql.yml':
                        self.assertIn('cache: false', step, f'{path.name} {name}')


class BundleBudget(unittest.TestCase):
    def test_e2e_checks_the_bundle_it_built(self):
        e2e = '\n'.join(dict(workflow_jobs(ROOT / '.github/workflows/ci.yml'))['e2e'])
        build = e2e.find('corepack pnpm exec vite build')
        check = e2e.find('corepack pnpm bundle:check')
        self.assertGreaterEqual(build, 0)
        self.assertGreater(check, build, 'bundle:check must run after the Vite build of the same job')


class ImageLayerCache(unittest.TestCase):
    def test_the_image_job_reads_the_layer_cache_and_never_writes_it(self):
        # Without it every run compiled the Go dependencies again: 52 s of a 117-second image job.
        # Writing it took 52 s more after a dependency change, so image-cache.yml writes it instead.
        image = '\n'.join(dict(workflow_jobs(ROOT / '.github/workflows/ci.yml'))['image'])
        self.assertIn('cache-from: type=gha,scope=image', image)
        self.assertNotIn('cache-to:', image)
        writer = (ROOT / '.github/workflows/image-cache.yml').read_text()
        # After CI, never beside it: a full main push already takes 20 runners with go-cache and Scorecard.
        self.assertIn('workflows: [CI]', writer)
        self.assertNotIn('push:', writer)
        self.assertIn('cache-to: type=gha,scope=image,mode=max', writer)

    def test_the_dependency_compile_depends_only_on_the_imported_packages(self):
        dockerfile = (ROOT / 'server/Dockerfile').read_text()
        go = dockerfile[dockerfile.index(' AS go\n'):]
        self.assertLess(go.index('go build -trimpath $(cat /deps.txt)'), go.index('COPY server ./'))

    def test_compose_smoke_runs_where_the_layers_are(self):
        jobs = dict(workflow_jobs(ROOT / '.github/workflows/ci.yml'))
        image = '\n'.join(jobs['image'])
        self.assertIn('start compose-smoke sh tests/compose-smoke.sh', image)
        self.assertIn('BUILDX_BUILDER: ${{ steps.buildx.outputs.name }}', image)
        self.assertIn('wait compose-smoke', image)
        self.assertNotIn('compose-smoke', '\n'.join(jobs['roadmap']))


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
