import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// docs/user/knowledge-base.md "Link an article in a reply". The reply is sent as it is stored, as
// text/plain (server/internal/email/compose_article_link_test.go); delivering it over SMTP is the
// email channel journey's (onboarding-email.spec.ts).
const baseURL = servers.main;

test('knowledge base: Articles in the reply box links a published article as its title and address on lines of their own, and the address opens it', async ({ page }) => {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Links Desk ${suffix}`, `admin-${suffix}@acme.example`, baseURL));
  const headers = { Origin: baseURL };
  const collection = await page.request.post(`${baseURL}/api/v1/kb/collections`, { headers, data: { name: 'Guides' } });
  expect(collection.status()).toBe(201);
  const title = `Reset your password ${suffix}`;
  const created = await page.request.post(`${baseURL}/api/v1/kb/articles`, {
    headers, data: { collection_id: (await collection.json() as { id: string }).id, title, body_md: 'Open **Settings**, then *Password*.', status: 'published' },
  });
  expect(created.status()).toBe(201);
  const { url } = await created.json() as { url: string };
  expect(url).toMatch(new RegExp(`^${baseURL}/help/[a-z0-9-]+/a/1-reset-your-password-`));
  const contact = await page.request.post(`${baseURL}/api/v1/contacts`, { headers, data: { name: 'Lan Customer', email: `lan-${suffix}@customer.example` } });
  expect(contact.status()).toBe(201);
  const conversation = await page.request.post(`${baseURL}/api/v1/conversations`, { headers, data: { subject: `Locked out ${suffix}`, contact_id: (await contact.json() as { id: string }).id } });
  expect(conversation.status()).toBe(201);
  const { id } = await conversation.json() as { id: string };

  await page.goto(`${baseURL}/conversations/${id}`);
  const body = page.getByLabel('Message body').last();
  await expect(body).toBeEnabled();
  await body.fill('Hi Lan, these steps help:\n\nBest regards');

  // While the help center is off nothing is public: the list says why and leads an admin to Settings.
  const articles = page.getByRole('button', { name: 'Articles' });
  await articles.click();
  const picker = page.getByRole('dialog', { name: 'Articles' });
  await expect(picker.getByText('The help center is off, so no article has a public page to link to.')).toBeVisible();
  await expect(picker.getByRole('button', { name: 'Open help center settings' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(picker).toBeHidden();
  await expect(body).toBeFocused();

  const on = await page.request.put(`${baseURL}/api/v1/help-center`, { headers, data: { enabled: true, title: '', locale: 'en' } });
  expect(on.status()).toBe(200);

  // From the keyboard: open the list, search at once, Enter puts the link where the cursor is.
  await body.evaluate((field: HTMLTextAreaElement) => field.setSelectionRange('Hi Lan, these steps help:\n'.length, 'Hi Lan, these steps help:\n'.length));
  await articles.focus();
  await page.keyboard.press('Enter');
  await page.getByRole('combobox', { name: 'Search published articles' }).fill('password');
  await expect(picker.getByRole('option', { name: title, selected: true })).toBeVisible();
  await page.keyboard.press('Enter');
  await expect(picker).toBeHidden();
  await expect(body).toBeFocused();
  const sent = `Hi Lan, these steps help:\n${title}\n${url}\nBest regards`;
  await expect(body).toHaveValue(sent);

  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(body).toHaveValue('');
  // What is stored is what the mail carries: the title and the address on lines of their own, no Markdown.
  const timeline = await (await page.request.get(`${baseURL}/api/v1/conversations/${id}/messages`)).json() as { items: Array<{ kind: string; body: string }> };
  expect(timeline.items.filter((message) => message.kind === 'outgoing').map((message) => message.body)).toEqual([sent]);

  // The address in the reply is the article on the public help center.
  const article = await page.request.get(url);
  expect(article.status()).toBe(200);
  expect(await article.text()).toContain(title);
});
