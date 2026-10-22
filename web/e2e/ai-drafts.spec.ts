import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { FakeAI } from './support/fake-ai';

// docs/user/ai-drafts.md: a customer's email gets a draft from the knowledge base, shown with its
// source, and a person sends it. The model is a local stand-in (support/fake-ai.ts).
const baseURL = servers.main;
const answer = 'Open Settings, then Billing, and choose Export invoices.';
const ai = new FakeAI(answer);
test.beforeAll(() => ai.start());
test.afterAll(() => ai.stop());

test('AI drafts: a customer email gets a draft from the knowledge base with its source, and a person sends it as written', async ({ page }) => {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`AI Desk ${suffix}`, `admin-${suffix}@acme.example`, baseURL));
  const headers = { Origin: baseURL };
  const collection = await page.request.post(`${baseURL}/api/v1/kb/collections`, { headers, data: { name: 'Billing' } });
  const title = `Export invoices ${suffix}`;
  const article = await page.request.post(`${baseURL}/api/v1/kb/articles`, {
    headers, data: { collection_id: (await collection.json() as { id: string }).id, title, body_md: answer, status: 'published' },
  });
  expect(article.status()).toBe(201);
  expect((await page.request.put(`${baseURL}/api/v1/channels/email`, { headers, data: { support_address: `support-${suffix}@acme.example` } })).status()).toBe(200);
  const { inbound_secret: secret } = await (await page.request.post(`${baseURL}/api/v1/channels/email/secret`, { headers })).json() as { inbound_secret: string };
  // The draft is written from what retrieval built; give it a moment to build the article.
  await expect.poll(async () => (await page.request.get(`${baseURL}/api/v1/workspace/ai`)).status()).toBe(200);
  await page.waitForTimeout(2500);

  const raw = [`From: Mia Customer <mia-${suffix}@customer.example>`, `To: support-${suffix}@acme.example`, `Subject: Invoices ${suffix}`,
    `Message-ID: <ai-${suffix}@customer.example>`, '', 'How do I download all my invoices?'].join('\r\n');
  const inbound = await page.request.post(`${baseURL}/api/v1/channels/email/inbound`, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret }, data: Buffer.from(raw),
  });
  const { conversation_id: id } = await inbound.json() as { conversation_id: string };

  await page.goto(`${baseURL}/conversations/${id}`);
  const card = page.getByRole('region', { name: 'AI draft' });
  await expect(card.getByText(answer)).toBeVisible({ timeout: 20_000 });
  await expect(card.getByText('From your knowledge base')).toBeVisible();
  await expect(card.getByRole('link', { name: `#1 ${title}` })).toBeVisible();
  expect(ai.prompts.some((prompt) => prompt.includes('How do I download all my invoices?') && prompt.includes(answer))).toBe(true);

  await card.getByRole('button', { name: 'Send' }).click();
  await expect(page.getByText('from an AI draft')).toBeVisible();
  await expect(card).toBeHidden();

  await page.goto(`${baseURL}/settings#ai-heading`);
  await expect(page.getByRole('heading', { name: 'AI drafts' })).toBeVisible();
  await expect(page.getByText(/1 sent unchanged/)).toBeVisible();
});
