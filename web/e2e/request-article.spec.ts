import { expect, test } from '@playwright/test';
import type { APIRequestContext } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// docs/user/inbox.md "Request an article" and docs/user/knowledge-base.md "Knowledge gaps", on the real server:
// a request from a conversation is two presses, shows in Knowledge gaps linked back to the conversation, and
// "Start a draft" opens the editor with that conversation beside the draft.
const baseURL = servers.main;
const headers = { Origin: baseURL };

async function openConversation(request: APIRequestContext, suffix: string, subject: string) {
  const contact = await request.post(`${baseURL}/api/v1/contacts`, { headers, data: { name: 'Lan Customer', email: `lan-${suffix}@customer.example` } });
  expect(contact.status()).toBe(201);
  const conversation = await request.post(`${baseURL}/api/v1/conversations`, { headers, data: { subject, contact_id: (await contact.json() as { id: string }).id } });
  expect(conversation.status()).toBe(201);
  return (await conversation.json() as { id: string }).id;
}

test('request article: a request from a conversation takes two presses and shows in Knowledge gaps, linked back to the conversation', async ({ page }) => {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Requests Desk ${suffix}`, `admin-${suffix}@acme.example`, baseURL));
  const subject = 'Export invoices to CSV'; // no long number: a request hides numbers of 6 digits or more
  const id = await openConversation(page.request, suffix, subject);

  await page.goto(`${baseURL}/conversations/${id}`);
  await page.getByRole('button', { name: 'Request article' }).click();
  const dialog = page.getByRole('dialog', { name: 'Request an article' });
  await expect(dialog.getByLabel('What should the article answer?')).toHaveValue(subject);
  await dialog.getByRole('button', { name: 'Request', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Article requested. It shows in Knowledge gaps.' })).toBeVisible();
  await expect(dialog).toBeHidden();

  await page.goto(`${baseURL}/knowledge/gaps`);
  const row = page.getByRole('listitem').filter({ has: page.getByRole('button', { name: subject }) });
  await expect(row).toContainText('Asked from 1 conversation');
  await expect(row).toContainText('Requested from a conversation');
  await row.getByRole('button', { name: subject }).click();
  const asked = row.getByRole('list', { name: 'Conversations that asked for it' });
  await expect(asked.getByRole('link', { name: new RegExp(subject) })).toHaveAttribute('href', `/conversations/${id}`);
});

test('request article: Start a draft opens the editor with the conversation that asked beside the draft', async ({ page }) => {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Drafts Desk ${suffix}`, `admin-${suffix}@acme.example`, baseURL));
  const collection = await page.request.post(`${baseURL}/api/v1/kb/collections`, { headers, data: { name: 'Guides' } });
  expect(collection.status()).toBe(201);
  const subject = 'Change the billing email';
  const id = await openConversation(page.request, suffix, subject);
  const filed = await page.request.post(`${baseURL}/api/v1/conversations/${id}/article-request`, { headers, data: {} });
  expect(filed.status()).toBe(201);

  await page.goto(`${baseURL}/knowledge/gaps`);
  const row = page.getByRole('listitem').filter({ has: page.getByRole('button', { name: subject }) });
  await row.getByRole('button', { name: subject }).click();
  await row.getByRole('button', { name: 'Start a draft' }).click();
  await expect(page).toHaveURL(/\/knowledge\/articles\/[0-9a-f-]{36}/);
  const sources = page.getByRole('region', { name: 'Sources' });
  await expect(sources).toContainText('Requested from these conversations.');
  await expect(sources.getByRole('link', { name: new RegExp(subject) })).toHaveAttribute('href', `/conversations/${id}`);
});
