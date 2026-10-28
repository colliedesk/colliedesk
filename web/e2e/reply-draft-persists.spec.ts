import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// An unsent reply is kept in this browser for its conversation: a reload, or Settings and the browser's
// Back, finds the text where it was. A sent reply leaves nothing behind.
test('an unsent reply survives a reload and the browser Back, and is gone once sent', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  await redeemLink(page, createWorkspaceWithSlug(`Draft Desk ${suffix}`, `owner-${suffix}@example.com`).url);
  const headers = { Origin: servers.main };
  const contact = await page.request.post(`${servers.main}/api/v1/contacts`, { headers, data: { name: 'Draft Customer', email: `draft-${suffix}@example.com` } });
  const { id: contactId } = await contact.json() as { id: string };
  const created = await page.request.post(`${servers.main}/api/v1/conversations`, { headers, data: { contact_id: contactId, subject: 'Draft question' } });
  const { id } = await created.json() as { id: string };

  await page.goto(`${servers.main}/conversations/${id}`);
  const body = page.getByLabel('Message body');
  await body.fill('A note I have not sent yet');
  await page.reload();
  await expect(body).toHaveValue('A note I have not sent yet');

  await page.goto(`${servers.main}/settings`);
  await page.goBack();
  await expect(body).toHaveValue('A note I have not sent yet');

  // Sent as a private note (no email needed); the box empties and a reload finds nothing.
  await page.getByRole('button', { name: /Private note/ }).click();
  await body.press('Control+Enter');
  await expect(page.getByText('A note I have not sent yet').first()).toBeVisible();
  await expect(body).toHaveValue('');
  await page.reload();
  await expect(body).toHaveValue('');
});
