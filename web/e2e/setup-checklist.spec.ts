import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// The setup guide stays in the inbox after the first conversation exists, and its email step turns
// to Done once a forwarded customer email arrives (docs/user/setup-guide.md).
const baseURL = servers.mail;

test('setup guide: a workspace with one conversation and no email channel still shows "Receive customer email", which reads done once a forwarded email arrives', async ({ page }) => {
  const suffix = uniqueSuffix();
  const support = `support-${suffix}@acme.example`;
  await redeemLink(page, createWorkspace(`Setup Desk ${suffix}`, `admin-${suffix}@acme.example`, baseURL));
  const headers = { Origin: baseURL };

  // A conversation made by hand, as an import or an AI agent would: the inbox is no longer empty.
  const contact = await page.request.post(`${baseURL}/api/v1/contacts`, { headers, data: { name: 'Setup Customer', email: `setup-${suffix}@customer.example` } });
  expect(contact.status()).toBe(201);
  const { id } = await contact.json() as { id: string };
  const made = await page.request.post(`${baseURL}/api/v1/conversations`, { headers, data: { contact_id: id, subject: `Made by hand ${suffix}` } });
  expect(made.status()).toBe(201);

  await page.goto(baseURL);
  await expect(page.getByRole('button', { name: new RegExp(`Made by hand ${suffix}`) })).toBeVisible();
  const guide = page.getByRole('region', { name: 'Finish setting up' });
  const email = guide.getByRole('listitem').filter({ hasText: 'Receive customer email' });
  await expect(email).toBeVisible();
  await expect(email).toHaveAttribute('data-state', 'todo');
  await expect(guide).toContainText('0 of 4');

  // The email channel is set up and a customer email is forwarded to it.
  const saved = await page.request.put(`${baseURL}/api/v1/channels/email`, { headers, data: { support_address: support } });
  expect(saved.status()).toBe(200);
  // A saved address without its inbound secret cannot receive yet: the step says so.
  await page.reload();
  await expect(email).toContainText('Needs attention');
  const rotated = await page.request.post(`${baseURL}/api/v1/channels/email/secret`, { headers });
  const { inbound_secret: secret } = await rotated.json() as { inbound_secret: string };
  await page.reload();
  await expect(email).toContainText('Waiting for the first email');
  const inbound = await page.request.post(`${baseURL}/api/v1/channels/email/inbound`, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret },
    data: Buffer.from([`From: Erin <erin-${suffix}@customer.example>`, `To: ${support}`, `Subject: Forwarded ${suffix}`, `Message-ID: <setup-${suffix}@customer.example>`, '', 'Hello.', ''].join('\r\n')),
  });
  expect(inbound.status()).toBe(200);

  await page.reload();
  await expect(email).toHaveAttribute('data-state', 'done');
  await expect(email).toContainText('Done');
  await expect(guide).toContainText('1 of 4');

  // Hide is remembered; Preferences brings the guide back.
  await guide.getByRole('button', { name: 'Hide' }).click();
  await expect(guide).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('button', { name: new RegExp(`Forwarded ${suffix}`) })).toBeVisible();
  await expect(guide).toHaveCount(0);
  await page.goto(`${baseURL}/settings`);
  const show = page.getByRole('checkbox', { name: 'Show the setup guide' });
  await expect(show).not.toBeChecked();
  await show.click();
  await expect(show).toBeChecked();
  await page.goto(baseURL);
  await expect(guide).toBeVisible();
});
