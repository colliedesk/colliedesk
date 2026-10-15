import { execFileSync } from 'node:child_process';
import { expect, test } from '@playwright/test';
import { databaseURL, servers } from './support/cli';

const baseURL = servers.main;

test('selects conversations with the keyboard and resolves them in one action', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const pairingURL = execFileSync('go', [
    'run', './cmd/colliedesk', 'create-workspace',
    '--name', `Bulk Desk ${suffix}`, '--admin-email', `admin-${suffix}@example.com`,
  ], {
    cwd: '../server', encoding: 'utf8',
    env: {
      ...process.env, APP_ORIGIN: baseURL, DATABASE_URL: databaseURL,
      GOPATH: '/tmp/colliedesk-gopath', GOMODCACHE: '/tmp/colliedesk-gopath/pkg/mod', GOCACHE: '/tmp/colliedesk-go-cache',
    },
  }).trim();
  await page.goto(pairingURL);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${baseURL}/`);

  const headers = { Origin: baseURL };
  const contact = await (await page.request.post(`${baseURL}/api/v1/contacts`, {
    headers, data: { name: 'Bulk Customer', email: `customer-${suffix}@example.com` },
  })).json() as { id: string };
  const ids: string[] = [];
  for (const subject of ['Bulk first', 'Bulk second', 'Bulk third']) {
    const created = await page.request.post(`${baseURL}/api/v1/conversations`, { headers, data: { contact_id: contact.id, subject } });
    expect(created.status()).toBe(201);
    ids.push(((await created.json()) as { id: string }).id);
  }
  const statusOf = async (id: string) => ((await (await page.request.get(`${baseURL}/api/v1/conversations/${id}`)).json()) as { status: string }).status;

  await page.reload();
  // Whole flow from the keyboard: Space ticks a row, Enter opens the menu, Tab and Enter choose.
  await page.getByRole('checkbox', { name: /^Select Bulk first/ }).focus();
  await page.keyboard.press('Space');
  await page.getByRole('checkbox', { name: /^Select Bulk third/ }).focus();
  await page.keyboard.press('Space');
  const bar = page.getByRole('group', { name: 'Bulk actions' });
  await expect(bar.getByText('2 selected')).toBeVisible();
  await bar.getByText('Set status', { exact: true }).focus();
  await page.keyboard.press('Enter');
  await page.keyboard.press('Tab'); // Open
  await page.keyboard.press('Tab'); // Pending
  await page.keyboard.press('Tab'); // Resolved
  await expect(bar.getByRole('button', { name: 'Resolved' })).toBeFocused();
  await page.keyboard.press('Enter');

  await expect(page.getByRole('status').filter({ hasText: 'Updated 2 conversations' })).toBeVisible();
  await expect(bar).toBeHidden();
  await expect(page.getByRole('checkbox', { name: 'Select all on this page' })).toBeFocused();
  const byStatus = await Promise.all(ids.map(statusOf));
  expect(byStatus).toEqual(['resolved', 'open', 'resolved']);
});
