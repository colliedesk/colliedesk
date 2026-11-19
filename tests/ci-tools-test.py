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
        # The job runs a line starting 'gates ' with go test -p 1: one package's gates at a time.
        self.assertTrue(patterns[-1].startswith('gates '), patterns[-1])
        patterns = [pattern.removeprefix('gates ').split(' ')[0] for pattern in patterns]
        for name in ['TestInboxListLatencyBudget', 'TestPendingAgentRequestsLatencyBudget',
                     'TestStorageUsageStaysFastWithAHugeAttachmentTable',
                     'TestTimelineReadWithHeavyHTMLMailStaysInBudget']:
            self.assertFalse(any(re.search(p, name) for p in patterns[:-1]))
            self.assertRegex(name, patterns[-1])
        self.assertNotRegex('TestAlpha', patterns[-1])

    def test_parts_split_a_regular_shard_and_never_the_gates(self):
        listing = self.listing + 'TestDelta\nTestEpsilon\nTestZeta\nTestInboxListLatencyBudget\nTestTrialCapStaysFast\n'
        timings = {'TestInboxListLatencyBudget': 20, 'TestTrialCapStaysFast': 20,
                   **{name: 1 for name in ['TestAlpha', 'TestBeta', 'TestGamma', 'TestDelta', 'TestEpsilon']}}
        whole = self.shards(listing, 2, timings=timings)
        parted = self.shards(listing, 2, '--parts', '2', timings=timings)
        self.assertEqual(len(whole[0]), 1)
        self.assertEqual(len(parted[0]), 2)
        for name in ['TestAlpha', 'TestBeta', 'TestGamma', 'TestDelta', 'TestEpsilon', 'TestZeta',
                     'FuzzBytes', 'ExampleClient']:
            self.assertRegex(name, whole[0][0])
        # The gates run in one process however many parts a regular shard has.
        self.assertEqual(parted[1], ['gates ^(TestInboxListLatencyBudget|TestTrialCapStaysFast)$ ./...'])
        self.assertEqual(whole[1], parted[1])
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
            shards.append(result.stdout.splitlines())
        for name in ['TestAlpha', 'TestBeta', 'TestGamma', 'TestDelta', 'FuzzBytes', 'ExampleClient'] + latency:
            hits = [i for i, patterns in enumerate(shards)
                    if any(re.search(p.removeprefix('gates ').split(' ')[0], name) for p in patterns)]
            self.assertEqual(len(hits), 1, name)
            self.assertEqual(hits[0] >= 2, name in latency, name)
        # One process on each latency runner, so a gate never measures beside another test binary
        # of its own shard; the tests that build the shared knowledge-base seed stay together.
        self.assertEqual([len(patterns) for patterns in shards[2:]], [1, 1])
        for name in latency[:3]:
            self.assertRegex(name, shards[2][0].removeprefix('gates ').split(' ')[0])
        for name in latency[3:]:
            self.assertRegex(name, shards[3][0].removeprefix('gates ').split(' ')[0])

    def shards(self, listing, total, *args, timings=None):
        """Each shard's pattern lines, after checking every name runs exactly once."""
        extra = []
        if timings is not None:
            path = pathlib.Path(self.tmp.name) / 'timings.json'
            path.write_text(json.dumps(timings))
            extra = ['--timings', str(path)]
        result = []
        for shard in range(1, total + 1):
            run_ = run('go-test-shard.py', str(shard), str(total), *args, *extra, stdin=listing)
            self.assertEqual(run_.returncode, 0, run_.stderr)
            result.append(run_.stdout.split('\n')[:-1])
        for name in {line.strip() for line in listing.splitlines()} - {''}:
            if re.fullmatch(r'(Test|Example|Fuzz)\w*', name):
                hits = [i for i, lines in enumerate(result) for p in lines
                        if p != '--' and re.search(p.removeprefix('gates ').split(' ')[0], name)]
                self.assertEqual(len(hits), 1, name)
        return result

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)

    def test_measured_times_balance_the_shards(self):
        # One long regular test fills a shard by itself; the short ones go to the other.
        short = [f'TestShort{i:02d}' for i in range(12)]
        listing = '\n'.join(['TestHeavy', *short, 'TestInboxListLatencyBudget', 'TestTrialCapStaysFast']) + '\n'
        timings = {'TestHeavy': 12, 'TestInboxListLatencyBudget': 6, 'TestTrialCapStaysFast': 6,
                   **{name: 1 for name in short}}
        shards = self.shards(listing, 3, '--latency-shards', '1', timings=timings)
        self.assertEqual(shards[0], ['^(TestHeavy)$'])
        self.assertEqual(shards[1], ['^(' + '|'.join(short) + ')$'])
        self.assertEqual(shards[2], ['gates ^(TestInboxListLatencyBudget|TestTrialCapStaysFast)$ ./...'])

    def test_measured_times_balance_the_parts_of_a_shard(self):
        listing = 'TestA\nTestB\nTestC\nTestD\nTestInboxListLatencyBudget\n'
        timings = {'TestA': 6, 'TestB': 3, 'TestC': 2, 'TestD': 1, 'TestInboxListLatencyBudget': 20}
        shards = self.shards(listing, 2, '--parts', '2', timings=timings)
        self.assertEqual(shards[0], ['^(TestA)$', '^(TestB|TestC|TestD)$'])

    def test_a_latency_runner_takes_regular_tests_only_after_its_gates_and_only_when_they_end_early(self):
        regular = [f'TestRegular{i:03d}' for i in range(40)]
        listing = '\n'.join(regular + ['TestHelpCenterPagesLatencyBudget', 'TestInboxListLatencyBudget']) + '\n'
        timings = {'TestHelpCenterPagesLatencyBudget': 3, 'TestInboxListLatencyBudget': 30,
                   **{name: 1 for name in regular}}
        shards = self.shards(listing, 4, '--latency-shards', '2', '--parts', '2', timings=timings)
        # The short gates leave room: the regular tests start once they finished (the '--' line).
        self.assertEqual(shards[2][0], 'gates ^(TestHelpCenterPagesLatencyBudget)$ ./...')
        self.assertEqual(shards[2][1], '--')
        self.assertEqual(len(shards[2][2:]), 2)
        self.assertFalse(any(re.search(shards[2][0].removeprefix('gates ').split(' ')[0], name) for name in regular))
        # The long gates take as long as a regular shard: gates only, in one process.
        self.assertEqual(shards[3], ['gates ^(TestInboxListLatencyBudget)$ ./...'])
        self.assertTrue(all('--' not in lines for lines in shards[:2]))

    def test_a_part_counts_its_overlapping_tests_and_its_process_start(self):
        regular = ['TestA', 'TestB', 'TestC', 'TestD', 'TestE', 'TestF']
        listing = '\n'.join(regular + ['TestInboxListLatencyBudget']) + '\n'
        timings = {'TestInboxListLatencyBudget': 8, **{name: 4 for name in regular}}
        # A part's packages run at once (go test -p), so its tests take about half their summed time;
        # a part pays 3 seconds to start its process. The gates run one after another.
        shards = self.shards(listing, 2, '--parallel', '2', '--process-seconds', '3', timings=timings)
        self.assertEqual(shards[0], ['^(TestA|TestB|TestC|TestD|TestE)$'])
        self.assertEqual(shards[1], ['gates ^(TestInboxListLatencyBudget)$ ./...', '--', '^(TestF)$'])
        for bad in (('--parallel', '0'), ('--process-seconds', '-1')):
            self.assertNotEqual(run('go-test-shard.py', '1', '2', *bad, stdin=listing).returncode, 0, bad)

    def test_gates_spread_by_time_and_the_kb_seed_stays_on_the_first_latency_runner(self):
        kb = ['TestHelpCenterPagesLatencyBudget', 'TestKnowledgeBaseSearchLatencyBudget']
        others = ['TestInboxListLatencyBudget', 'TestStorageUsageStaysFast', 'TestTrialCapStaysFast']
        listing = '\n'.join(['TestAlpha', *kb, *others]) + '\n'
        timings = {kb[0]: 5, kb[1]: 5, others[0]: 8, others[1]: 4, others[2]: 4, 'TestAlpha': 1}
        shards = self.shards(listing, 3, '--latency-shards', '2', timings=timings)
        # 10 seconds of seed gates; 8, 4 and 4 longest first onto the runner that ends earlier.
        self.assertEqual(shards[1], ['gates ^(TestHelpCenterPagesLatencyBudget|TestKnowledgeBaseSearchLatencyBudget|TestTrialCapStaysFast)$ ./...'])
        self.assertEqual(shards[2], ['gates ^(TestInboxListLatencyBudget|TestStorageUsageStaysFast)$ ./...'])

    def test_tests_without_a_measurement_weigh_the_median(self):
        listing = 'TestOld1\nTestOld2\nTestOld3\nTestOld4\nTestNew1\nTestNew2\nTestInboxListLatencyBudget\n'
        timings = {'TestOld1': 1, 'TestOld2': 2, 'TestOld3': 2, 'TestOld4': 9, 'TestInboxListLatencyBudget': 30}
        shards = self.shards(listing, 3, '--latency-shards', '1', timings=timings)
        # The median is 2: TestOld4 (9) alone; 2+2+2+2+1 on the other shard.
        self.assertEqual(shards[0], ['^(TestOld4)$'])
        self.assertEqual(shards[1], ['^(TestNew1|TestNew2|TestOld1|TestOld2|TestOld3)$'])

    def test_a_broken_timings_file_fails_closed(self):
        for content in ['not json', '[]', '{"TestA": -1}', '{"TestA": "slow"}']:
            path = pathlib.Path(self.tmp.name) / 'bad.json'
            path.write_text(content)
            result = run('go-test-shard.py', '1', '2', '--timings', str(path), stdin=self.listing)
            self.assertNotEqual(result.returncode, 0, content)
        missing = run('go-test-shard.py', '1', '2', '--timings', str(pathlib.Path(self.tmp.name) / 'none.json'),
                      stdin=self.listing)
        self.assertNotEqual(missing.returncode, 0)

    def test_the_go_job_balances_by_the_checked_in_timings(self):
        go = '\n'.join(dict(workflow_jobs(ROOT / '.github/workflows/ci.yml'))['go'])
        self.assertIn('--timings ../.github/go-test-timings.json', go)
        timings = json.loads((ROOT / '.github/go-test-timings.json').read_text())
        self.assertGreater(len(timings), 100)
        # -v prints each test's time, which go-test-timings.py reads back from the job logs.
        self.assertRegex(go, r'go test -count=1 -run "\$pattern" -v "\$\{packages\[@\]\}" &')

    def test_the_go_job_starts_a_latency_shards_regular_tests_only_after_its_gates_exit(self):
        go = '\n'.join(dict(workflow_jobs(ROOT / '.github/workflows/ci.yml'))['go'])
        script = re.search(r'- name: Test\n(?:        .*\n)*?        run: \|\n((?:          .*\n?)+)', go).group(1)
        script = '\n'.join(line[10:] for line in script.splitlines())
        with tempfile.TemporaryDirectory() as tmp:
            tmp = pathlib.Path(tmp)
            # A stand-in go: the gate sleeps, a regular pattern fails, each logs start and end.
            # It logs its flags too: the gates run one package at a time (-p 1), regular patterns at once.
            (tmp / 'go').write_text('#!/bin/bash\necho "start $4 ${*:5}" >> "$LOG"\n'
                                    '[ "$4" = GATE ] && sleep 0.5\necho "end $4" >> "$LOG"\n'
                                    '[ "$4" = B ] && exit 3\nexit 0\n')
            (tmp / 'go').chmod(0o755)
            runner = tmp / 'temp'
            runner.mkdir()
            (runner / 'go-test-patterns.txt').write_text('gates GATE example.com/m/httpapi\n--\nA\nB\n')
            env = dict(os.environ, PATH=f'{tmp}:{os.environ["PATH"]}', RUNNER_TEMP=str(runner), LOG=str(tmp / 'log'))
            result = subprocess.run(['bash', '-e', '-c', script], env=env, capture_output=True, text=True)
            log = (tmp / 'log').read_text().split('\n')
        self.assertNotEqual(result.returncode, 0, 'a failing pattern after the gates fails the step')
        self.assertIn('start GATE -v -p 1 example.com/m/httpapi', log)
        self.assertLess(log.index('end GATE'), log.index('start A -v ./...'), log)
        self.assertLess(log.index('end GATE'), log.index('start B -v ./...'), log)
        self.assertFalse(any(line.startswith('start --') for line in log), log)

    def test_the_gates_line_names_only_the_packages_that_hold_gates(self):
        # go test -list prints a package's names, then 'ok <package> <time>'. With -p 1 every package
        # named starts its test binary in turn, so the gates line names only the packages with gates.
        listing = ('TestAlpha\nTestInboxListLatencyBudget\nok  \texample.com/m/httpapi\t0.10s\n'
                   'TestBeta\nok  \texample.com/m/store\t0.01s\n'
                   '? \texample.com/m/empty\t[no test files]\n'
                   'TestImportStatusLatencyBudget\nok  \texample.com/m/importjobs\t0.02s\n')
        shards = self.shards(listing, 2)
        self.assertEqual(shards[1][0], 'gates ^(TestImportStatusLatencyBudget|TestInboxListLatencyBudget)$ '
                                       'example.com/m/httpapi example.com/m/importjobs')

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


class GoTimings(unittest.TestCase):
    """go-test-timings.py reads the go jobs' -v logs back into the file go-test-shard.py balances by."""
    log = (
        'go (1)\tTest\t2026-10-23T04:40:01.1Z === RUN   TestAlpha\n'
        'go (1)\tTest\t2026-10-23T04:40:01.2Z --- PASS: TestAlpha (1.50s)\n'
        'go (1)\tTest\t2026-10-23T04:40:01.3Z     --- PASS: TestAlpha/sub (9.00s)\n'
        'go (1)\tTest\t2026-10-23T04:40:01.4Z --- FAIL: TestBeta (2.25s)\n'
        'go (2)\tTest\t2026-10-23T04:40:01.5Z --- SKIP: TestGamma (0.00s)\n'
        'go (2)\tTest\t2026-10-23T04:40:01.6Z --- PASS: TestAlpha (0.50s)\n'
        'go (2)\tTest\t2026-10-23T04:40:01.7Z ok  \tgithub.com/x/pkg\t3.1s\n'
        'go (2)\tTest\t2026-10-23T04:40:01.8Z --- PASS: ExampleClient (0.01s)\n'
        'go (2)\tTest\t2026-10-23T04:40:01.9Z noise --- PASS: TestNoise (5.00s)\n'
    )

    def timings(self, *logs):
        with tempfile.TemporaryDirectory() as tmp:
            paths = []
            for i, log in enumerate(logs):
                path = pathlib.Path(tmp) / f'{i}.log'
                path.write_text(log)
                paths.append(str(path))
            return run('go-test-timings.py', *paths)

    def test_top_level_times_summed_over_packages(self):
        result = self.timings(self.log)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout),
                         {'ExampleClient': 0.01, 'TestAlpha': 2.0, 'TestBeta': 2.25, 'TestGamma': 0.0})
        self.assertEqual(result.stdout, json.dumps(json.loads(result.stdout), indent=1, sort_keys=True) + '\n')

    def test_several_runs_take_the_median_and_a_name_in_one_run_keeps_its_time(self):
        # A slow runner or a run beside heavier tests slows everything it measures; the median of
        # several runs keeps one such run from setting a test's weight.
        runs = [f'go (3)\tTest\t2026-10-23T05:00:00.0Z --- PASS: TestAlpha ({s:.2f}s)\n' for s in (4, 30)]
        result = self.timings(self.log, *runs)
        self.assertEqual(result.returncode, 0, result.stderr)
        timings = json.loads(result.stdout)
        self.assertEqual(timings['TestAlpha'], 4.0)
        self.assertEqual(timings['TestBeta'], 2.25)
        two = json.loads(self.timings(self.log, runs[0]).stdout)
        self.assertEqual(two['TestAlpha'], 3.0)

    def test_a_log_without_test_times_fails_closed(self):
        for log in ['', 'go (1)\tTest\t2026-10-23T04:40:01.7Z ok  \tgithub.com/x/pkg\t3.1s\n']:
            result = self.timings(log)
            self.assertNotEqual(result.returncode, 0, log)
            self.assertEqual(result.stdout, '')


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


FAKE_PLAYWRIGHT = '''import json, os, re, sys
tests = json.loads(os.environ['FAKE_E2E_TESTS'])
broken = os.environ.get('FAKE_E2E_BREAK', '')
if '--list' not in sys.argv or '--reporter=json' not in sys.argv:
    sys.exit('fake playwright: expected --list --reporter=json')
files = [a for a in sys.argv[1:] if len(a) > 1 and a.startswith('/') and a.endswith('/')]
if broken == 'listing' or (files and broken == 'filtered-listing'):
    sys.exit('fake playwright: listing failed')
listed, shift = tests, 0
if files:
    # Playwright matches a file filter against the test file's absolute path.
    pattern = re.compile(files[-1][1:-1])
    listed = [t for t in tests if pattern.search('/repo/web/e2e/' + t[0])]
    if broken == 'ignore-filter':
        listed = tests
    if broken.startswith('drop:'):
        listed = [t for t in listed if t[1] != broken[5:]]
    if broken.startswith('add:') and listed:
        listed = listed + [['d.spec.ts', broken[4:]]]
    shift = int(os.environ.get('FAKE_E2E_SHIFT', '0'))
suites = {}
for file, title, *line in listed:
    suites.setdefault(file, []).append({'title': title, 'file': file, 'line': (line or [1])[0] + shift,
                                        'column': 1, 'tests': [{'projectName': ''}]})
print('npm noise before the report')
print(json.dumps({'suites': [{'title': f, 'file': f, 'specs': specs, 'suites': []} for f, specs in suites.items()],
                  'errors': []}))
'''


def e2e_listing(files):
    """A Playwright --list --reporter=json report with one test per (file, title)."""
    suites = {}
    for file, title in files:
        suites.setdefault(file, []).append({'title': title, 'file': file, 'line': 1, 'column': 1,
                                            'tests': [{'projectName': 'chromium'}]})
    return json.dumps({'suites': [{'title': f, 'file': f, 'specs': s, 'suites': []} for f, s in suites.items()],
                       'errors': []})


class E2ESplit(unittest.TestCase):
    """e2e-shard.py gives each shard whole test files by measured time, longest first, as one file
    filter for the Playwright command line."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)

    def split(self, files, shard, shards, timings, *args, listing=None):
        path = pathlib.Path(self.tmp.name) / 'timings.json'
        path.write_text(timings if isinstance(timings, str) else json.dumps(timings))
        stdin = listing if listing is not None else e2e_listing([(f, 'a test') for f in files])
        return run('e2e-shard.py', str(shard), str(shards), '--timings', str(path), *args, stdin=stdin)

    def filters(self, files, shards, timings, *args):
        result = []
        for shard in range(1, shards + 1):
            out = self.split(files, shard, shards, timings, *args)
            self.assertEqual(out.returncode, 0, out.stderr)
            result.append(out.stdout.strip())
        return result

    def test_measured_times_balance_the_shards_longest_first(self):
        # 10 to the first shard, 6 and then 5 to the second (6 < 10), 4 to the first (10 < 11).
        timings = {'a.spec.ts': 10, 'b.spec.ts': 6, 'c.spec.ts': 5, 'd.spec.ts': 4}
        self.assertEqual(self.filters(['d.spec.ts', 'c.spec.ts', 'b.spec.ts', 'a.spec.ts'], 2, timings),
                         [r'/(^|\/)(a\.spec\.ts|d\.spec\.ts)$/', r'/(^|\/)(b\.spec\.ts|c\.spec\.ts)$/'])

    def test_the_last_shard_starts_later_so_it_takes_less(self):
        # Two tests at once, and the last shard starts its tests 3 seconds after the others: a ends
        # at 5 on the first; b at 3+3=6 on the last; c at 5+2.5=7.5 on the first; d at 6+2=8 on the last.
        timings = {'a.spec.ts': 10, 'b.spec.ts': 6, 'c.spec.ts': 5, 'd.spec.ts': 4}
        self.assertEqual(self.filters(list(timings), 2, timings, '--parallel', '2', '--last-shard-delay', '3'),
                         [r'/(^|\/)(a\.spec\.ts|c\.spec\.ts)$/', r'/(^|\/)(b\.spec\.ts|d\.spec\.ts)$/'])

    def test_a_file_without_a_measurement_weighs_the_median_of_the_measured_ones(self):
        # The median of 10, 9 and 1 is 9: new.spec.ts joins b (9 < 10) and c then fits on the first.
        timings = {'a.spec.ts': 10, 'b.spec.ts': 9, 'c.spec.ts': 1, 'gone.spec.ts': 50}
        self.assertEqual(self.filters(['a.spec.ts', 'b.spec.ts', 'c.spec.ts', 'new.spec.ts'], 2, timings),
                         [r'/(^|\/)(a\.spec\.ts|c\.spec\.ts)$/', r'/(^|\/)(b\.spec\.ts|new\.spec\.ts)$/'])

    def test_every_listed_file_matches_exactly_one_shard_and_nothing_else_matches(self):
        files = [f'feature-{i:02d}.spec.ts' for i in range(20)] + ['x.y.spec.ts']
        timings = {name: (i * 7) % 11 + 1 for i, name in enumerate(files)}
        patterns = [re.compile(f[1:-1]) for f in self.filters(files, 3, timings)]
        for name in files:
            self.assertEqual(sum(bool(p.search('/home/runner/work/web/e2e/' + name)) for p in patterns), 1, name)
        for other in ['prefix-feature-01.spec.ts', 'feature-01.spec.ts.bak', 'xzy.spec.ts', 'onboarding-signup.spec.ts']:
            self.assertFalse(any(p.search('/home/runner/work/web/e2e/' + other) for p in patterns), other)

    def test_it_reports_the_predicted_time_of_every_shard(self):
        timings = {'a.spec.ts': 10, 'b.spec.ts': 6, 'c.spec.ts': 5}
        result = self.split(['a.spec.ts', 'b.spec.ts', 'c.spec.ts', 'new.spec.ts'], 1, 2, timings)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('3 of 4 files measured', result.stderr)
        # new.spec.ts weighs the median, 6: a 10 and c 5 on the first; b 6 and new 6 on the second.
        self.assertIn('shard 1/2: 2 files, 15s', result.stderr)
        self.assertIn('shard 2/2: 2 files, 12s', result.stderr)

    def test_an_empty_shard_or_an_unusable_input_fails_closed(self):
        timings = {'a.spec.ts': 1, 'b.spec.ts': 1}
        # An empty filter would run every test, so a shard without files is an error.
        cases = [
            ('a shard has no files', dict(files=['a.spec.ts'], shard=2, shards=2, timings=timings)),
            ('the listing has no tests', dict(files=[], shard=1, shards=1, timings=timings)),
            ('the timings name none', dict(files=['e2e/a.spec.ts', 'e2e/b.spec.ts'], shard=1, shards=2, timings=timings)),
            ('timings: ', dict(files=['a.spec.ts'], shard=1, shards=1, timings='{')),
            ('non-negative seconds', dict(files=['a.spec.ts'], shard=1, shards=1, timings={'a.spec.ts': -1})),
            ('shard must be between', dict(files=['a.spec.ts', 'b.spec.ts'], shard=3, shards=2, timings=timings)),
            ('shard must be between', dict(files=['a.spec.ts'], shard=0, shards=0, timings=timings)),
            # Playwright reads a colon in a file filter as a line number.
            ('a file name has a colon', dict(files=['a:1.spec.ts'], shard=1, shards=1, timings={'a:1.spec.ts': 1})),
            ('not a JSON report', dict(files=None, shard=1, shards=1, timings=timings, listing='no json here')),
            ('reported errors', dict(files=None, shard=1, shards=1, timings=timings,
                                     listing=json.dumps({'suites': [], 'errors': [{'message': 'syntax'}]}))),
        ]
        for reason, case in cases:
            result = self.split(case['files'] or [], case['shard'], case['shards'], case['timings'],
                                listing=case.get('listing'))
            self.assertNotEqual(result.returncode, 0, reason)
            self.assertIn(reason, result.stderr)
            self.assertEqual(result.stdout, '', reason)
        for bad in (('--parallel', '0'), ('--last-shard-delay', '-1')):
            result = self.split(['a.spec.ts', 'b.spec.ts'], 1, 2, timings, *bad)
            self.assertEqual(result.returncode, 2, bad)
            self.assertIn('parallel must be positive and the delay non-negative', result.stderr)


class E2ETimings(unittest.TestCase):
    """e2e-test-timings.py reads the e2e jobs' logs back into the file e2e-shard.py balances by."""
    log = (
        '2026-11-19T08:57:18.7663864Z   ✓    1 [chromium] › e2e/ai-chat.spec.ts:79:1 › asks a question (5.1s)\n'
        '2026-11-19T08:57:19.1000000Z   ✓    2 [chromium] › e2e/ai-chat.spec.ts:90:1 › keeps the thread (853ms)\n'
        '2026-11-19T08:57:20.0000000Z   ✘    3 [chromium] › e2e/channels.spec.ts:12:3 › Mail › replies (30.0s)\n'
        '2026-11-19T08:57:51.0000000Z   ✓    4 [chromium] › e2e/channels.spec.ts:12:3 › Mail › replies (retry #1) (1.2m)\n'
        '2026-11-19T08:57:52.0000000Z   -    5 [chromium] › e2e/skipped.spec.ts:3:1 › later\n'
        '2026-11-19T08:57:53.0000000Z   ✓    6 [chromium] › e2e/nested/deep.spec.ts:3:1 › a › b › c (2.0s)\n'
        # gh run view --log puts the job and the step before each line.
        'e2e (2)\tRun end-to-end workflows\t2026-11-19T16:09:06.2750436Z   ✓    1 [chromium] › e2e/nested/deep.spec.ts:9:3 › d (0.5s)\n'
        '2026-11-19T08:57:54.0000000Z noise ✓ 7 [chromium] e2e/noise.spec.ts (9.0s)\n'
        '2026-11-19T08:57:55.0000000Z   101 passed (3.9m)\n'
    )

    def timings(self, *logs):
        with tempfile.TemporaryDirectory() as tmp:
            paths = []
            for i, log in enumerate(logs):
                path = pathlib.Path(tmp) / f'{i}.log'
                path.write_text(log, encoding='utf-8')
                paths.append(str(path))
            return run('e2e-test-timings.py', *paths)

    def test_seconds_per_file_summed_over_its_tests_and_retries_relative_to_the_test_dir(self):
        result = self.timings(self.log)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout),
                         {'ai-chat.spec.ts': 5.95, 'channels.spec.ts': 102.0, 'nested/deep.spec.ts': 2.5})
        self.assertEqual(result.stdout, json.dumps(json.loads(result.stdout), indent=1, sort_keys=True) + '\n')

    def test_several_runs_take_the_median(self):
        runs = [f'2026-11-19T09:00:00.0Z   ✓    1 [chromium] › e2e/ai-chat.spec.ts:79:1 › x ({s}s)\n' for s in (4, 40)]
        timings = json.loads(self.timings(self.log, *runs).stdout)
        self.assertEqual(timings['ai-chat.spec.ts'], 5.95)
        self.assertEqual(timings['channels.spec.ts'], 102.0)

    def test_a_log_without_test_times_fails_closed(self):
        for log in ['', '2026-11-19T08:57:55.0000000Z   101 passed (3.9m)\n']:
            result = self.timings(log)
            self.assertNotEqual(result.returncode, 0, log)
            self.assertIn('no test times in the logs', result.stderr)
            self.assertEqual(result.stdout, '')


class E2EShards(unittest.TestCase):
    """The browser tests run in shards: every test outside the onboarding jobs runs in exactly one
    shard, so splitting the job never drops or doubles a test. The check lists every shard through
    the file filter e2e-shard.py gives it, as the job's run does."""
    tests = [['a.spec.ts', 'one'], ['a.spec.ts', 'two'], ['b.spec.ts', 'three'], ['c.spec.ts', 'four']]
    measured = {'a.spec.ts': 6, 'b.spec.ts': 3, 'c.spec.ts': 3}

    def check(self, tests=None, shards='2', timings=None, **env):
        with tempfile.TemporaryDirectory() as tmp:
            fake = pathlib.Path(tmp) / 'playwright.py'
            fake.write_text(FAKE_PLAYWRIGHT)
            path = pathlib.Path(tmp) / 'timings.json'
            path.write_text(json.dumps(self.measured if timings is None else timings))
            env = {**os.environ, 'FAKE_E2E_TESTS': json.dumps(self.tests if tests is None else tests), **env}
            return subprocess.run([sys.executable, str(ROOT / '.github/scripts/e2e-shard-check.py'), shards,
                                   '--timings', str(path), '--parallel', '2',
                                   '--', sys.executable, str(fake), '--grep-invert', '@onboarding'],
                                  text=True, capture_output=True, env=env)

    def test_shards_that_cover_every_test_once_pass(self):
        result = self.check()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('2 shards run all 4 tests exactly once', result.stdout)

    def test_a_test_reported_at_another_line_is_still_the_same_test(self):
        # Run 39904337642 listed bulk-actions.spec.ts at line 7 unsharded and line 5 in its shard.
        result = self.check(FAKE_E2E_SHIFT='-2')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_a_missing_or_doubled_test_fails_and_names_it(self):
        for broken, culprit in [('drop:three', 'three'), ('ignore-filter', 'runs 2 times'), ('add:five', 'five')]:
            result = self.check(FAKE_E2E_BREAK=broken)
            self.assertNotEqual(result.returncode, 0, broken)
            self.assertIn(culprit, result.stderr, broken)

    def test_no_tests_a_failed_listing_a_failed_split_or_a_bad_shard_count_fail_closed(self):
        for reason, kwargs in [('lists no tests', dict(tests=[])),
                               ('listing all tests failed', dict(FAKE_E2E_BREAK='listing')),
                               ('listing shard 1/2 failed', dict(FAKE_E2E_BREAK='filtered-listing')),
                               ('the timings name none', dict(timings={'other.spec.ts': 1})),
                               ('a shard has no files', dict(shards='4')),
                               ('shards must be at least 1', dict(shards='0')),
                               ('shards must be at least 1', dict(shards='x'))]:
            result = self.check(**kwargs)
            self.assertNotEqual(result.returncode, 0, reason)
            self.assertIn(reason, result.stderr)

    def test_the_e2e_job_runs_its_shard_with_the_arguments_the_check_lists(self):
        e2e = '\n'.join(dict(workflow_jobs(ROOT / '.github/workflows/ci.yml'))['e2e'])
        self.assertRegex(e2e, r'(?m)^        shard: \[1, 2\]$')
        self.assertRegex(e2e, r'(?m)^    timeout-minutes: 10$')
        # Pull requests run Chromium only; WebKit and Firefox run once a day (cross-browser.yml).
        self.assertRegex(e2e, r"(?m)^      E2E_ARGS: --project=chromium --grep-invert @onboarding --fully-parallel --workers=3$")
        self.assertRegex(e2e, r"(?m)^      E2E_SPLIT: --timings \.\./\.github/e2e-test-timings\.json --parallel [0-9.]+ --last-shard-delay [0-9.]+$")
        listing = 'corepack pnpm exec playwright test $E2E_ARGS'
        self.assertIn(f'{listing} --list --reporter=json | python3 ../.github/scripts/e2e-shard.py "$SHARD" "$SHARDS" $E2E_SPLIT', e2e)
        self.assertIn(f'python3 ../.github/scripts/e2e-shard-check.py "$SHARDS" $E2E_SPLIT -- {listing}', e2e)
        self.assertIn('corepack pnpm e2e $E2E_ARGS "$FILES"', e2e)
        self.assertNotIn('--shard=', e2e)
        self.assertEqual(e2e.count('SHARDS: ${{ strategy.job-total }}'), 3, 'the split, the run and the check use the matrix size')
        self.assertIn('ci-background.sh wait shards', e2e, 'the job waits for the check it started')
        self.assertEqual(e2e.count('corepack pnpm e2e '), 1, 'one Playwright run per shard')

    def test_the_checked_in_timings_name_spec_files_relative_to_the_test_dir(self):
        # A key that never matches a listed file weighs nothing; e2e-shard.py then falls back to the
        # median for every file, and the shards would split by file count without failing.
        timings = json.loads((ROOT / '.github/e2e-test-timings.json').read_text())
        specs = {p.name for p in (ROOT / 'web/e2e').glob('*.spec.ts')}
        self.assertGreater(len(timings), 30)
        self.assertLessEqual(set(timings), specs)
        self.assertTrue(all(isinstance(s, (int, float)) and s >= 0 for s in timings.values()))


class Budget(unittest.TestCase):
    def test_ci_has_no_phase_budget_and_cd_keeps_its_60_seconds(self):
        # The owner exempted Colliedesk CI from the 120-second phase budget; CD keeps its 60.
        ci = (ROOT / '.github/workflows/ci.yml').read_text()
        self.assertNotIn('ci-budget.py', ci)
        for name in ['ci.yml', 'codeql.yml', 'go-cache.yml']:
            text = (ROOT / '.github/workflows' / name).read_text()
            self.assertNotRegex(text, r'(?m)^\s+timeout-minutes: 2$', name)
        self.assertRegex((ROOT / '.github/workflows/publish-edge.yml').read_text(),
                         r'(?m)^    timeout-minutes: 1$')

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
        # 8 Go shards, web, 2 e2e shards, 2 onboarding, image, roadmap, 3 CodeQL; not pr-verified,
        # and not osv-scan, which starts when roadmap ends.
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
        # One shard uploads, a latency shard (6, 7 and 8 of 8).
        self.assertIn('matrix.shard == 7', condition)
        self.assertIn("!startsWith(steps.go-cache.outputs.cache-matched-key, format('go-{0}-{1}-{2}-', "
                      "runner.os, steps.go.outputs.go-version, hashFiles('go.sum')))", condition)
        go = '\n'.join(dict(workflow_jobs(ROOT / '.github/workflows/ci.yml'))['go'])
        self.assertIn('id: go-cache', go)
        self.assertRegex(go, r"latency-shards 3\b")
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

    def test_a_ci_run_that_timed_out_still_writes_the_layer_cache(self):
        # A cold image job passes the 2-minute timeout, and GitHub ends that run as cancelled. Skipping
        # cancelled runs left main cold for good: run 39893295025 timed out, 39893508473 was skipped.
        image_cache = '\n'.join(dict(workflow_jobs(ROOT / '.github/workflows/image-cache.yml'))['image-cache'])
        condition = ' '.join(line.strip() for line in
                             re.search(r'^    if: >-\n((?:      .*\n)+)', image_cache + '\n', re.M).group(1).splitlines())
        self.assertNotIn('conclusion', condition)
        for guard in ["github.event_name == 'workflow_dispatch'",
                      "github.event.workflow_run.event == 'push'",
                      'github.event.workflow_run.head_repository.full_name == github.repository']:
            self.assertIn(guard, condition)

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
