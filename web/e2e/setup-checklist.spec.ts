import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { openQuickStart, redeemLink } from './support/browser';

// Quick start stays beside the workspace name after the first conversation exists, and its email step turns to Done
// once a forwarded customer email arrives (docs/user/setup-guide.md).
const baseURL = servers.mail;

test('Quick start: a workspace with one conversation and no email channel still shows "Receive customer email", which reads done once a forwarded email arrives', async ({ page }) => {
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
  // With a conversation in the inbox Quick start waits beside the workspace name; one press opens its checklist.
  const quickStart = page.getByRole('navigation', { name: 'Main' }).getByRole('button', { name: /^Quick start/ });
  await expect(quickStart).toHaveAccessibleName('Quick start, 5 steps left');
  let guide = await openQuickStart(page);
  const email = () => guide.getByRole('listitem').filter({ hasText: 'Receive customer email' });
  await expect(email()).toBeVisible();
  await expect(email()).toHaveAttribute('data-state', 'todo');
  await expect(guide).toContainText('0 of 5');

  // The email channel is set up and a customer email is forwarded to it.
  const saved = await page.request.put(`${baseURL}/api/v1/channels/email`, { headers, data: { support_address: support } });
  expect(saved.status()).toBe(200);
  // A saved address without its inbound secret cannot receive yet: the step says so.
  await page.reload();
  guide = await openQuickStart(page);
  await expect(email()).toContainText('Needs attention');
  const rotated = await page.request.post(`${baseURL}/api/v1/channels/email/secret`, { headers });
  const { inbound_secret: secret } = await rotated.json() as { inbound_secret: string };
  await page.reload();
  guide = await openQuickStart(page);
  await expect(email()).toContainText('Waiting for the first email');
  const inbound = await page.request.post(`${baseURL}/api/v1/channels/email/inbound`, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret },
    data: Buffer.from([`From: Erin <erin-${suffix}@customer.example>`, `To: ${support}`, `Subject: Forwarded ${suffix}`, `Message-ID: <setup-${suffix}@customer.example>`, '', 'Hello.', ''].join('\r\n')),
  });
  expect(inbound.status()).toBe(200);

  await page.reload();
  guide = await openQuickStart(page);
  await expect(email()).toHaveAttribute('data-state', 'done');
  await expect(email()).toContainText('Done');
  await expect(guide).toContainText('1 of 5');
  await expect(quickStart).toHaveAccessibleName('Quick start, 4 steps left');

  // Skip these leaves Try more out of the count, and the browser keeps it; Show these brings it back.
  await guide.getByRole('button', { name: 'Skip these' }).click();
  await expect(guide).toContainText('1 of 3');
  await page.reload();
  await expect(quickStart).toHaveAccessibleName('Quick start, 2 steps left');
  guide = await openQuickStart(page);
  await guide.getByRole('button', { name: 'Show these' }).click();
  await expect(quickStart).toHaveAccessibleName('Quick start, 4 steps left');

  // Hiding the setup guide in Preferences hides Quick start; turning it back on brings it back.
  await page.goto(`${baseURL}/settings`);
  const show = page.getByRole('checkbox', { name: 'Show the setup guide' });
  await expect(show).toBeChecked();
  await show.click();
  await expect(show).not.toBeChecked();
  await expect(quickStart).toHaveCount(0);
  await page.goto(baseURL);
  await expect(page.getByRole('button', { name: new RegExp(`Forwarded ${suffix}`) })).toBeVisible();
  await expect(quickStart).toHaveCount(0);
  await page.goto(`${baseURL}/settings`);
  await show.click();
  await expect(show).toBeChecked();
  await expect(quickStart).toBeVisible();
});
