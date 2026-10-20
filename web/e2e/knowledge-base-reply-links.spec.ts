import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { mailpit, waitForMail } from './support/mailpit';

// docs/user/knowledge-base.md "Link an article in a reply", on the server with a real SMTP relay (Mailpit).
const baseURL = servers.mail;

test('knowledge base: Articles in the reply box links a published article as its title and address, the mail carries both as plain text and the address opens it', async ({ page }) => {
  const suffix = uniqueSuffix();
  const support = `support-${suffix}@acme.example`;
  const customer = `lan-${suffix}@customer.example`;
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

  await page.getByRole('link', { name: 'Settings' }).click();
  await page.getByRole('textbox', { name: 'Support address' }).fill(support);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await page.getByRole('button', { name: 'Create inbound secret' }).click();
  const secret = await page.getByRole('textbox', { name: /Inbound secret/ }).inputValue();
  const inbound = await page.request.post(`${baseURL}/api/v1/channels/email/inbound`, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret },
    data: Buffer.from([
      `From: Lan Customer <${customer}>`, `To: ${support}`, `Subject: Locked out ${suffix}`, `Message-ID: <locked-${suffix}@customer.example>`,
      'MIME-Version: 1.0', 'Content-Type: text/plain; charset=utf-8', '', 'I forgot my password.', '',
    ].join('\r\n')),
  });
  expect(inbound.status()).toBe(200);

  await page.getByRole('link', { name: 'Inbox' }).click();
  await page.getByRole('button', { name: new RegExp(`Locked out ${suffix}`) }).click();
  const body = page.getByLabel('Message body').last();
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

  // From the keyboard: open the list, search, Enter puts the link where the cursor is.
  await body.evaluate((field: HTMLTextAreaElement) => field.setSelectionRange('Hi Lan, these steps help:\n'.length, 'Hi Lan, these steps help:\n'.length));
  await articles.focus();
  await page.keyboard.press('Enter');
  await page.getByRole('combobox', { name: 'Search published articles' }).fill('password');
  await expect(picker.getByRole('option', { name: title, selected: true })).toBeVisible();
  await page.keyboard.press('Enter');
  await expect(picker).toBeHidden();
  await expect(body).toBeFocused();
  await expect(body).toHaveValue(`Hi Lan, these steps help:\n${title}\n${url}\nBest regards`);

  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(page.getByText('Sent', { exact: true })).toBeVisible({ timeout: 30_000 });
  const delivered = await waitForMail(customer);
  expect(delivered.text.replace(/\r\n/g, '\n')).toContain(`these steps help:\n${title}\n${url}\n`);
  const html = await (await fetch(`${mailpit.api}/api/v1/message/${delivered.id}`)).json() as { HTML: string };
  expect(html.HTML).toBe('');
  expect(delivered.text).not.toContain('](');

  // The address in the mail is the article on the public help center.
  const article = await page.request.get(url);
  expect(article.status()).toBe(200);
  expect(await article.text()).toContain(title);
});
