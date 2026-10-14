import { describe, expect, test } from 'vitest';
import { checkOnboardingJourneys, compareJourneys } from './check-onboarding-journeys.mjs';

const agents = [
  '# Project', '', '## Onboarding journeys', '',
  '- `self-host first run: works` (`web/e2e/a.spec.ts`)', '- `invite teammate: works` (`web/e2e/b.spec.ts`)', '',
  '## Next section', '- `not a journey`',
].join('\n');

describe('compareJourneys', () => {
  test('passes when every listed journey is a tagged test and every tagged test is listed', () => {
    const specs = { 'a.spec.ts': "test('self-host first run: works', { tag: '@onboarding' }, async () => {});",
      'b.spec.ts': "test('invite teammate: works', { tag: '@onboarding' }, async ({ page }) => {});" };
    expect(compareJourneys(agents, specs)).toEqual({ missing: [], unlisted: [], disabled: [] });
  });

  test('reports a listed journey whose test was deleted, renamed or lost its tag', () => {
    const specs = { 'a.spec.ts': "test('self-host first run: works', { tag: '@onboarding' }, async () => {});",
      'b.spec.ts': "test('invite teammate: works', async () => {});" };
    expect(compareJourneys(agents, specs).missing).toEqual(['invite teammate: works']);
  });

  test('reports a tagged test that AGENTS.md does not list', () => {
    const specs = { 'a.spec.ts': "test('self-host first run: works', { tag: '@onboarding' }, async () => {});\ntest('invite teammate: works', { tag: '@onboarding' }, async () => {});\ntest('it\\'s new', { tag: '@onboarding' }, async () => {});" };
    expect(compareJourneys(agents, specs).unlisted).toEqual(["it's new"]);
  });

  test.each([
    ["  test.skip(true, 'flaky');"], ['  test.fixme();'], ["test.only('x', async () => {});"], ['test.fail();'],
    ["test.describe.skip('group', () => {});"], ["test.describe.only('group', () => {});"],
  ])('reports a spec with onboarding journeys that disables or narrows tests: %s', (line) => {
    const specs = { 'a.spec.ts': `test('self-host first run: works', { tag: '@onboarding' }, async () => {\n${line}\n});`,
      'b.spec.ts': "test('invite teammate: works', { tag: '@onboarding' }, async () => {});" };
    expect(compareJourneys(agents, specs).disabled).toEqual(['a.spec.ts']);
  });
});

test('AGENTS.md lists exactly the onboarding journey tests in web/e2e', async () => {
  expect(await checkOnboardingJourneys()).toEqual({ missing: [], unlisted: [], disabled: [] });
});
