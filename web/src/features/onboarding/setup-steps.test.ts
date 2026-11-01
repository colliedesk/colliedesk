import { expect, test } from 'vitest';
import { guideHasWork } from './setup-steps';
import type { SetupProgress } from '../../api/schemas';

test('a skipped move remains reachable to restore its optional steps, only for admins', () => {
  const progress = { steps: [
    { key: 'import', state: 'done', detail: null, url: '/settings/import-freshdesk' },
    { key: 'preview', state: 'done', detail: null, url: 'https://acme.colliedesk.com/help' },
    { key: 'custom_domain', state: 'skipped', detail: null, url: '/settings#help-center-domain-heading' },
  ], complete: true, recommended: null, next_url: null } satisfies SetupProgress;
  expect(guideHasWork(progress, true)).toBe(true);
  expect(guideHasWork(progress, false)).toBe(false);
});
