import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// Issue #101 with two real browsers (docs/user/inbox.md, "See who else is on a conversation"): Bao sees
// Lan writing, Lan's reply goes first, Bao's reply started before it is held with Lan's reply on screen
// and goes on Send anyway; when Lan leaves the page Bao stops seeing her.
const baseURL = servers.main;
const headers = { Origin: baseURL };

test('two people on one conversation: who is writing, a warning before a second reply, and leaving', async ({ browser }) => {
  const suffix = uniqueSuffix();
  const lanContext = await browser.newContext();
  const lan = await lanContext.newPage();
  await redeemLink(lan, createWorkspace(`Collision Desk ${suffix}`, `lan-${suffix}@example.com`));
  expect((await lan.request.patch(`${baseURL}/api/v1/agents/me/display-name`, { headers, data: { display_name: 'Lan' } })).status()).toBe(200);
  const invite = await lan.request.post(`${baseURL}/api/v1/auth/invites`, { headers, data: { email: `bao-${suffix}@example.com`, role_ceiling: 'agent' } });
  expect(invite.status()).toBe(201);
  const contact = await (await lan.request.post(`${baseURL}/api/v1/contacts`, { headers, data: { name: 'Mai', email: `mai-${suffix}@example.com` } })).json() as { id: string };
  const conversation = await (await lan.request.post(`${baseURL}/api/v1/conversations`, { headers, data: { contact_id: contact.id, subject: 'Where is my refund?' } })).json() as { id: string };

  const baoContext = await browser.newContext();
  const bao = await baoContext.newPage();
  await redeemLink(bao, (await invite.json() as { url: string }).url);
  expect((await bao.request.patch(`${baseURL}/api/v1/agents/me/display-name`, { headers, data: { display_name: 'Bao' } })).status()).toBe(200);

  const open = `${baseURL}/conversations/${conversation.id}`;
  await lan.goto(open);
  await bao.goto(open);
  await expect(bao.getByRole('listitem').getByText('Lan is viewing')).toBeVisible({ timeout: 15_000 });

  // Bao starts a reply; then Lan starts hers, and Bao sees it.
  await bao.getByLabel('Message body').fill('We are checking your refund.');
  await lan.getByLabel('Message body').fill('Your refund left today.');
  await expect(bao.getByRole('listitem').getByText('Lan is writing a reply')).toBeVisible({ timeout: 15_000 });

  // Lan sends first; Bao, who started before Lan's reply, is held with Lan's reply on screen.
  await lan.getByRole('button', { name: 'Send reply' }).click();
  await expect(lan.getByLabel('Message body')).toHaveValue('');
  await bao.getByRole('button', { name: 'Send reply' }).click();
  const warning = bao.getByRole('alert').filter({ hasText: 'Someone replied while you were writing' });
  await expect(warning).toBeVisible();
  await expect(warning).toContainText('Your refund left today.');
  await expect(bao.getByLabel('Message body')).toHaveValue('We are checking your refund.');
  await warning.getByRole('button', { name: 'Send anyway' }).click();
  await expect(bao.getByLabel('Message body')).toHaveValue('');
  const replies = await (await bao.request.get(`${baseURL}/api/v1/conversations/${conversation.id}/messages`)).json() as { items: Array<{ body: string; reply_number: number | null }> };
  expect(replies.items.map((item) => `${item.reply_number}:${item.body}`)).toEqual(['1:Your refund left today.', '2:We are checking your refund.']);

  // Lan leaves the page. The leave goes as the page unloads (fetch keepalive, which the server takes only
  // with this instance's Origin), so Bao stops seeing her at his next heartbeat, 5 seconds, well before her
  // row would expire on its own (15 seconds).
  await lan.goto(`${baseURL}/settings`);
  await expect(bao.getByRole('listitem').getByText(/^Lan is/)).toHaveCount(0, { timeout: 9_000 });

  await lanContext.close();
  await baoContext.close();
});
