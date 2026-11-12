import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// Issue #459 with two real browsers (docs/user/mentions.md): Tu asks Long in a private note with @, sees
// that Long was told, Long finds it in Mentions, opening it marks it read, and Tu then reads that Long saw it.
const baseURL = servers.main;
const headers = { Origin: baseURL };

test('a private note asks a teammate with @: Mentions counts it, opening it marks it read, the writer sees it was seen', async ({ browser }, testInfo) => {
  const suffix = uniqueSuffix();
  const tuContext = await browser.newContext();
  const tu = await tuContext.newPage();
  await redeemLink(tu, createWorkspace(`Mentions Desk ${suffix}`, `tu-${suffix}@example.com`));
  expect((await tu.request.patch(`${baseURL}/api/v1/agents/me/display-name`, { headers, data: { display_name: 'Hoang Tu' } })).status()).toBe(200);
  const invite = await tu.request.post(`${baseURL}/api/v1/auth/invites`, { headers, data: { email: `long-${suffix}@example.com`, roles: ['Agent'] } });
  expect(invite.status()).toBe(201);
  const contact = await (await tu.request.post(`${baseURL}/api/v1/contacts`, { headers, data: { name: 'Hana Sato', email: `hana-${suffix}@example.com` } })).json() as { id: string };
  const conversation = await (await tu.request.post(`${baseURL}/api/v1/conversations`, { headers, data: { contact_id: contact.id, subject: 'Refund still not on my card' } })).json() as { id: string };

  const longContext = await browser.newContext();
  const long = await longContext.newPage();
  await redeemLink(long, (await invite.json() as { url: string }).url);
  expect((await long.request.patch(`${baseURL}/api/v1/agents/me/display-name`, { headers, data: { display_name: 'Long Bui' } })).status()).toBe(200);

  // Tu types @ in a private note and picks Long from the list under the box.
  await tu.goto(`${baseURL}/conversations/${conversation.id}`);
  await tu.getByRole('button', { name: /^Private note/ }).click();
  await expect(tu.getByText('Not visible to the customer · Type @ to ask a teammate')).toBeVisible();
  const box = tu.getByLabel('Message body');
  await box.click();
  await box.pressSequentially('Bank confirmed RF-2291-08 lands by Thursday. @Lo');
  const list = tu.getByRole('listbox', { name: 'Told in Mentions and by email' });
  await expect(list.getByRole('option', { name: /Long Bui/ })).toBeVisible();
  await tu.screenshot({ path: testInfo.outputPath('i9a-picker.png') });
  await box.press('Enter');
  await box.pressSequentially('can you check the provider?');
  await expect(box).toHaveValue('Bank confirmed RF-2291-08 lands by Thursday. @Long Bui can you check the provider?');
  await tu.getByRole('button', { name: /Add private note/ }).click();
  await expect(tu.getByText('Long Bui was told · in Mentions and by email')).toBeVisible();
  await expect(tu.locator('.message-note strong', { hasText: '@Long Bui' })).toBeVisible();
  await tu.screenshot({ path: testInfo.outputPath('i9b-receipt.png') });

  // Long sees the count in the sidebar, opens Mentions, then the row.
  await long.goto(`${baseURL}/settings`);
  const mentionsLink = long.getByRole('link', { name: 'Mentions, 1 unread' });
  await expect(mentionsLink).toBeVisible({ timeout: 15_000 });
  await mentionsLink.click();
  await expect(long.getByRole('button', { name: 'Unread 1' })).toBeVisible();
  const row = long.getByRole('link', { name: /Hoang Tu.*Refund still not on my card/ });
  await row.click();
  await expect(long.getByText('Hoang Tu asked you · marked read when you opened it')).toBeVisible();
  await expect(long.getByRole('link', { name: 'Mentions' })).toBeVisible({ timeout: 15_000 });
  await long.screenshot({ path: testInfo.outputPath('i9c-mentions.png') });

  // Tu reads that Long saw it.
  await tu.reload();
  await expect(tu.getByText(/^Long Bui saw this /)).toBeVisible();

  await tuContext.close();
  await longContext.close();
});
