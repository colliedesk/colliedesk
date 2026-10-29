import { expect, test } from '@playwright/test';
import { servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { FakeFreshdesk, fixture } from './support/fake-freshdesk';
import { createWorkspaceWithSlug } from './support/workspace';

// Journey: docs/user/import-freshdesk.md, "Import in the app". The real server runs the import in the
// background against the local Freshdesk API v2 (the only fake), with the same counts as the CLI journey.
const baseURL = servers.main;
const apiKey = 'fd-e2e-in-app-key-never-shown';

test('in-app freshdesk import: an admin reads the account, imports it in the background, reconcile OK with exact counts, and undoes it', { tag: '@onboarding' }, async ({ page }) => {
  test.setTimeout(240_000);
  const suffix = uniqueSuffix();
  const adminEmail = `alice-${suffix}@acme.example`;
  const workspace = createWorkspaceWithSlug(`In-app Import ${suffix}`, adminEmail);
  const fake = new FakeFreshdesk({ apiKey, agentEmail: adminEmail, fixed: true });
  await fake.start();
  try {
    await redeemLink(page, workspace.url);
    // The empty inbox leads an admin to the import page.
    await page.getByRole('region', { name: 'Finish setting up' }).getByRole('link', { name: /Import from Freshdesk/ }).click();
    await expect(page).toHaveURL(`${baseURL}/settings/import-freshdesk`);
    await expect(page.getByRole('heading', { name: 'Import from Freshdesk', level: 1 })).toBeVisible();

    await page.getByLabel('Freshdesk address').fill('acme.freshdesk.com');
    await page.getByLabel('API key of a Freshdesk admin').fill(apiKey);
    await page.getByRole('button', { name: 'Read account' }).click();
    await expect(page.getByRole('heading', { name: 'Found in acme.freshdesk.com' })).toBeVisible({ timeout: 60_000 });
    await expect(page.getByRole('row', { name: /Tickets/ })).toContainText(`at least ${fixture.tickets - 1}`);
    await expect(page.getByRole('row', { name: /Contacts/ })).toContainText(String(fixture.contacts));
    // The Freshdesk agents come along: the admin is linked by email, the former agent can be invited.
    const agents = page.getByRole('region', { name: 'Freshdesk agents' });
    await expect(agents.getByRole('listitem').filter({ hasText: 'Alice Agent' })).toContainText('Linked to');
    await expect(agents.getByRole('listitem').filter({ hasText: 'Bob Former' }).getByRole('button', { name: 'Invite' })).toBeVisible();
    // Nothing is written by the preview.
    const before = await (await page.request.get(`${baseURL}/api/v1/conversations`)).json() as { items: unknown[] };
    expect(before.items).toHaveLength(0);

    await page.getByRole('button', { name: 'Start import' }).click();
    await expect(page.getByRole('heading', { name: 'Import complete' })).toBeVisible({ timeout: 180_000 });
    const tickets = page.getByRole('row', { name: /^Tickets/ });
    await expect(tickets.getByRole('cell')).toHaveText(['300', '300', '—']);
    await expect(page.getByRole('row', { name: /^Messages/ }).getByRole('cell')).toHaveText(['960', '960', '—']);
    await expect(page.getByRole('row', { name: /^Attachments/ }).getByRole('cell')).toHaveText(['40', '40', '—']);
    await expect(page.getByRole('link', { name: 'Download report' })).toHaveAttribute('href', /\/report\?format=markdown$/);
    expect(await page.content()).not.toContain(apiKey);

    // The imported conversations are in the inbox with the original timestamps' order.
    const list = await (await page.request.get(`${baseURL}/api/v1/conversations?page_size=100`)).json() as { items: Array<{ channel: string }> };
    expect(list.items.length).toBe(100);
    expect(list.items.every((item) => item.channel === 'imported')).toBe(true);

    // Undo asks first, then removes exactly the import.
    await page.getByRole('button', { name: 'Undo import' }).click();
    await page.getByRole('button', { name: 'Remove imported data' }).click();
    await expect(page.getByText(/^Import removed: 300 conversations and 120 contacts\./)).toBeVisible({ timeout: 120_000 });
    // The way back in: a new import.
    await expect(page.getByRole('button', { name: 'New import' })).toBeVisible();
    const after = await (await page.request.get(`${baseURL}/api/v1/conversations`)).json() as { items: unknown[] };
    expect(after.items).toHaveLength(0);
  } finally {
    await fake.stop();
  }
});
