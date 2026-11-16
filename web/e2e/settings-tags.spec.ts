import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// docs/user/tags.md: an admin renames and deletes a tag in Settings → Tags, and every conversation follows.
test('settings tags: rename a tag everywhere, then delete it after the row says how many conversations lose it', async ({ page }) => {
  const suffix = uniqueSuffix();
  const base = servers.main;
  const headers = { Origin: base };
  await redeemLink(page, createWorkspace(`Tags ${suffix}`, `admin-${suffix}@example.com`));
  const contact = await (await page.request.post(`${base}/api/v1/contacts`, { headers, data: { name: 'Customer', email: `c-${suffix}@example.com` } })).json() as { id: string };
  const tag = await (await page.request.post(`${base}/api/v1/tags`, { headers: { ...headers, 'Idempotency-Key': `tag-${suffix}` }, data: { name: 'refnd' } })).json() as { id: string };
  const conversations: string[] = [];
  for (const subject of ['Refund please', 'Where is my refund']) {
    const conversation = await (await page.request.post(`${base}/api/v1/conversations`, { headers, data: { contact_id: contact.id, subject } })).json() as { id: string };
    expect((await page.request.put(`${base}/api/v1/conversations/${conversation.id}/tags/${tag.id}`, { headers })).status()).toBe(200);
    conversations.push(conversation.id);
  }

  await page.goto(`${base}/settings/tags`);
  const row = page.getByRole('listitem').filter({ hasText: 'refnd' });
  await expect(row).toContainText('2 conversations');

  await page.getByRole('button', { name: 'Rename refnd' }).click();
  await page.getByRole('textbox', { name: 'New name for refnd' }).fill('refund');
  await page.getByRole('textbox', { name: 'New name for refnd' }).press('Enter');
  await expect(page.getByRole('status')).toHaveText('Renamed “refnd” to “refund”.');
  const renamed = await (await page.request.get(`${base}/api/v1/conversations/${conversations[0]}`, { headers })).json() as { tags: { name: string }[] };
  expect(renamed.tags.map((item) => item.name)).toEqual(['refund']);

  await page.getByRole('button', { name: 'Delete refund' }).click();
  await expect(page.getByRole('group', { name: 'Delete refund' })).toContainText('Delete “refund”? It comes off 2 conversations. This cannot be undone.');
  await page.getByRole('button', { name: 'Delete tag' }).click();
  await expect(page.getByRole('status')).toHaveText('Deleted “refund”.');
  await expect(page.getByText('No tags yet. Add one from the details of a conversation.')).toBeVisible();
  for (const id of conversations) {
    const after = await (await page.request.get(`${base}/api/v1/conversations/${id}`, { headers })).json() as { tags: unknown[] };
    expect(after.tags).toEqual([]);
  }
});
