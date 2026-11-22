import { describe, expect, test } from 'vitest';
import { checkJourneys, compareJourneys, journeyLists } from './check-journeys.mjs';

const onboarding = { section: 'Onboarding journeys', tag: '@onboarding' };
const daily = { section: 'Daily-use journeys', tag: '@journey' };

const agents = [
  '# Project', '', '## Onboarding journeys', '',
  '- `self-host first run: works` (`web/e2e/a.spec.ts`)', '- `invite teammate: works` (`web/e2e/b.spec.ts`)', '',
  '## Daily-use journeys', '',
  '- `search: opens the chosen conversation` (`web/e2e/c.spec.ts`)', '',
  '## Next section', '- `not a journey`',
].join('\n');

const dailySpec = { 'c.spec.ts': "test('search: opens the chosen conversation', { tag: '@journey' }, async () => {});" };

describe('compareJourneys', () => {
  test('passes when every listed journey is a tagged test and every tagged test is listed', () => {
    const specs = { 'a.spec.ts': "test('self-host first run: works', { tag: '@onboarding' }, async () => {});",
      'b.spec.ts': "test('invite teammate: works', { tag: '@onboarding' }, async ({ page }) => {});", ...dailySpec };
    expect(compareJourneys(agents, specs, onboarding)).toEqual({ missing: [], unlisted: [], disabled: [] });
    expect(compareJourneys(agents, specs, daily)).toEqual({ missing: [], unlisted: [], disabled: [] });
  });

  test('reports a listed journey whose test was deleted, renamed or lost its tag', () => {
    const specs = { 'a.spec.ts': "test('self-host first run: works', { tag: '@onboarding' }, async () => {});",
      'b.spec.ts': "test('invite teammate: works', async () => {});",
      'c.spec.ts': "test('search: opens the chosen conversation', async () => {});" };
    expect(compareJourneys(agents, specs, onboarding).missing).toEqual(['invite teammate: works']);
    expect(compareJourneys(agents, specs, daily).missing).toEqual(['search: opens the chosen conversation']);
  });

  test('reports a tagged test that the journey list does not list', () => {
    const specs = { 'a.spec.ts': "test('self-host first run: works', { tag: '@onboarding' }, async () => {});\ntest('invite teammate: works', { tag: '@onboarding' }, async () => {});\ntest('it\\'s new', { tag: '@onboarding' }, async () => {});",
      'c.spec.ts': "test('search: opens the chosen conversation', { tag: '@journey' }, async () => {});\ntest('kb: a new one', { tag: '@journey' }, async () => {});" };
    expect(compareJourneys(agents, specs, onboarding).unlisted).toEqual(["it's new"]);
    expect(compareJourneys(agents, specs, daily).unlisted).toEqual(['kb: a new one']);
  });

  test('a daily-use journey tagged @onboarding instead counts for neither list it does not belong to', () => {
    const specs = { 'a.spec.ts': "test('self-host first run: works', { tag: '@onboarding' }, async () => {});\ntest('invite teammate: works', { tag: '@onboarding' }, async () => {});",
      'c.spec.ts': "test('search: opens the chosen conversation', { tag: '@onboarding' }, async () => {});" };
    expect(compareJourneys(agents, specs, daily).missing).toEqual(['search: opens the chosen conversation']);
    expect(compareJourneys(agents, specs, onboarding).unlisted).toEqual(['search: opens the chosen conversation']);
  });

  test.each([
    ["  test.skip(true, 'flaky');"], ['  test.fixme();'], ["test.only('x', async () => {});"], ['test.fail();'],
    ["test.describe.skip('group', () => {});"], ["test.describe.only('group', () => {});"],
  ])('reports a spec with journeys that disables or narrows tests: %s', (line) => {
    const specs = { 'a.spec.ts': `test('self-host first run: works', { tag: '@onboarding' }, async () => {\n${line}\n});`,
      'b.spec.ts': "test('invite teammate: works', { tag: '@onboarding' }, async () => {});",
      'c.spec.ts': `test('search: opens the chosen conversation', { tag: '@journey' }, async () => {\n${line}\n});` };
    expect(compareJourneys(agents, specs, onboarding).disabled).toEqual(['a.spec.ts']);
    expect(compareJourneys(agents, specs, daily).disabled).toEqual(['c.spec.ts']);
  });
});

test('the checked lists are the onboarding and the daily-use journeys', () => {
  expect(journeyLists).toEqual([onboarding, daily]);
});

test('docs/internals/journeys.md lists exactly the journey tests in web/e2e, for each list', async () => {
  expect(await checkJourneys()).toEqual({
    '@onboarding': { missing: [], unlisted: [], disabled: [] },
    '@journey': { missing: [], unlisted: [], disabled: [] },
  });
});
