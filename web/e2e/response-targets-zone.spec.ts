import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// docs/user/response-targets.md: the hours count in the workspace's timezone unless one is picked, while
// Reports shows dates in the reader's own. When the two differ the page says so and offers the reader's
// zone first, so an admin cannot take the clock for Reports' zone.
const baseURL = servers.main;

const patch = (page: Page, path: string, body: unknown) => page.evaluate(async ({ path: url, body: data }) => {
  const response = await fetch(url, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) });
  return response.status;
}, { path, body });
const clockZone = (page: Page) => page.evaluate(async () => ((await (await fetch('/api/v1/response-targets')).json()) as { timezone: string | null }).timezone);

test('hours on a UTC workspace read by someone on Asia/Tokyo: both zones are named, their own is the first choice, and choosing it makes the two agree', async ({ page }) => {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Zone Desk ${suffix}`, `admin-${suffix}@example.com`, baseURL));
  expect(await patch(page, '/api/v1/workspace/timezone', { timezone: 'UTC' })).toBe(200);
  expect(await patch(page, '/api/v1/agents/me/timezone', { timezone: 'Asia/Tokyo' })).toBe(200);
  await page.goto(`${baseURL}/settings/targets`);
  // Still off: the card that offers the turn-on already says which zone the hours will count in.
  const off = page.getByRole('region', { name: 'Response targets are off' });
  await expect(off.getByText(/^Starts with: .*, UTC · first reply/)).toBeVisible();
  await expect(off.getByText('Hours count in UTC; your dates show Asia/Tokyo.')).toBeVisible();
  await page.getByRole('button', { name: 'Turn on response targets' }).click();

  const hours = page.getByRole('region', { name: 'Business hours' });
  await expect(hours.getByText('Hours count in UTC; your dates show Asia/Tokyo.')).toBeVisible();
  const zone = hours.getByLabel('Time zone');
  await expect(zone.locator('option').nth(0)).toHaveText('The workspace’s (UTC)');
  await expect(zone.locator('option').nth(1)).toHaveText('Your time zone (Asia/Tokyo)');
  expect(await clockZone(page)).toBeNull();

  await zone.selectOption('Asia/Tokyo');
  await zone.press('Enter');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(hours.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  await expect(hours.getByText(/Hours count in/)).toHaveCount(0);
  expect(await clockZone(page)).toBe('Asia/Tokyo');

  await page.reload();
  await expect(page.getByRole('region', { name: 'Business hours' }).getByLabel('Time zone')).toHaveValue('Asia/Tokyo');
});
