import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// docs/user/knowledge-base.md, "Review before publishing": with approval required, the author cannot
// publish alone; a teammate who may publish approves from Reviews, then the author publishes.
const baseURL = servers.main;
const headers = { Origin: baseURL };

test('knowledge base review: the author sends a draft for review, the reviewer approves it from Reviews, the author publishes it', async ({ browser }) => {
  const suffix = uniqueSuffix();
  const authorContext = await browser.newContext();
  const author = await authorContext.newPage();
  await redeemLink(author, createWorkspace(`KB Review ${suffix}`, `ada-${suffix}@example.com`));
  expect((await author.request.patch(`${baseURL}/api/v1/agents/me/display-name`, { headers, data: { display_name: 'Ada Lane' } })).status()).toBe(200);
  expect((await author.request.put(`${baseURL}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Help', locale: 'en', require_approval: true } })).status()).toBe(200);
  const { id: billing } = await (await author.request.post(`${baseURL}/api/v1/kb/collections`, { headers, data: { name: 'Billing' } })).json() as { id: string };
  const article = await (await author.request.post(`${baseURL}/api/v1/kb/articles`, { headers,
    data: { collection_id: billing, title: 'Get a VAT invoice', body_md: 'Ask within 30 days.' } })).json() as { id: string; url: string };
  const invite = await author.request.post(`${baseURL}/api/v1/auth/invites`, { headers, data: { email: `long-${suffix}@example.com`, roles: ['Agent'] } });
  expect(invite.status()).toBe(201);
  const reviewerContext = await browser.newContext();
  const reviewer = await reviewerContext.newPage();
  await redeemLink(reviewer, (await invite.json() as { url: string }).url);
  expect((await reviewer.request.patch(`${baseURL}/api/v1/agents/me/display-name`, { headers, data: { display_name: 'Long Bui' } })).status()).toBe(200);

  // The author cannot publish alone: Publish says why, and Send for review asks a teammate who may publish.
  await author.goto(`${baseURL}/knowledge/articles/${article.id}`);
  await expect(author.getByText('Publishing needs a teammate’s approval: send it for review.').first()).toBeVisible();
  await expect(author.getByRole('button', { name: 'Publish', exact: true }).first()).toBeDisabled();
  await author.getByRole('button', { name: 'Send for review' }).click();
  const dialog = author.getByRole('dialog', { name: 'Send for review' });
  await dialog.getByRole('checkbox', { name: 'Long Bui' }).check();
  await dialog.getByRole('textbox', { name: 'Note to the reviewers' }).fill('Check the deadline');
  await dialog.getByRole('button', { name: 'Send for review' }).click();
  await expect(author.getByText('Waiting for approval from Long Bui.').first()).toBeVisible();
  expect((await author.request.get(article.url)).status()).toBe(404);

  // The reviewer finds it under Reviews, reads the change and approves it.
  await reviewer.goto(`${baseURL}/knowledge`);
  await reviewer.getByRole('link', { name: /^Reviews\b.*1 waiting for you$/ }).click();
  await reviewer.getByRole('link', { name: 'Get a VAT invoice' }).click();
  await expect(reviewer.getByText('Ask within 30 days.').first()).toBeVisible();
  await reviewer.getByRole('button', { name: 'Approve' }).click();
  await expect(reviewer.getByText('Approved', { exact: true }).first()).toBeVisible();

  // The author publishes the approved text; the help center shows it.
  await author.reload();
  await expect(author.getByText('Approved by Long Bui. Publish it now.').first()).toBeVisible();
  await author.getByRole('button', { name: 'Publish', exact: true }).first().click();
  const published = async () => (await (await author.request.get(`${baseURL}/api/v1/kb/articles/${article.id}`)).json() as { status: string; url: string });
  await expect.poll(async () => (await published()).status).toBe('published');
  const live = await author.request.get((await published()).url);
  expect(live.status()).toBe(200);
  expect(await live.text()).toContain('Ask within 30 days.');

  // History names each step with the person who took it.
  const history = await (await author.request.get(`${baseURL}/api/v1/kb/articles/${article.id}/versions`)).json() as { items: { kind: string; status: string; changed_by: { name: string } }[] };
  expect(history.items.map((item) => [item.kind, item.status, item.changed_by.name])).toEqual([
    ['edit', 'published', 'Ada Lane'], ['review_approve', 'draft', 'Long Bui'], ['review_request', 'draft', 'Ada Lane'], ['create', 'draft', 'Ada Lane']]);
  await reviewerContext.close();
  await authorContext.close();
});
