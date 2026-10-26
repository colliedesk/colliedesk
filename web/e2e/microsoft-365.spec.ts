import type { ChildProcess } from 'node:child_process';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { colliedesk, databaseURL, mustColliedesk, psql, startServer, stopServer, uniqueSuffix, withDatabase } from './support/cli';
import { ports } from './support/checkout';
import { redeemLink } from './support/browser';
import { microsoftStandIn, sentThroughMicrosoft, standIn, startMicrosoftStandIn } from './support/microsoft';

// Microsoft 365 sending against the local Microsoft stand-in, on a server and database of its own: the
// shared e2e servers run outbox workers without Microsoft settings, which would fail these replies.
const port = ports.microsoft;
const origin = `http://localhost:${port}`;
const database = `colliedesk_e2e_microsoft_${process.pid}`;
const msURL = withDatabase(databaseURL, database);
let server: ChildProcess | undefined;
let fake: ChildProcess | undefined;

test.beforeAll(async () => {
  psql(`DROP DATABASE IF EXISTS ${database}`, withDatabase(databaseURL, 'postgres'));
  psql(`CREATE DATABASE ${database}`, withDatabase(databaseURL, 'postgres'));
  expect(mustColliedesk(['migrate'], { databaseURL: msURL, origin })).toBe('migrations applied');
  fake = await startMicrosoftStandIn();
  server = await startServer(port, { DATABASE_URL: msURL, APP_ORIGIN: origin, MAILBOX_TOKEN_KEYS: 'k1:ZTJlLW1pY3Jvc29mdC0zNjUta2V5LTMyLWJ5dGVzISE=', ...microsoftStandIn.env() }, 'e2e');
});

test.afterAll(async () => {
  await stopServer(server);
  fake?.kill('SIGTERM');
  psql(`DROP DATABASE IF EXISTS ${database} WITH (FORCE)`, withDatabase(databaseURL, 'postgres'));
});

// signIn creates a workspace on the Microsoft server and opens it as its admin.
async function signIn(page: Page, name: string): Promise<string> {
  const suffix = uniqueSuffix();
  const created = colliedesk(['create-workspace', '--name', `${name} ${suffix}`, '--admin-email', `admin-${suffix}@example.com`], { databaseURL: msURL, origin });
  expect(created.status, created.stderr).toBe(0);
  await redeemLink(page, created.stdout.trim());
  return suffix;
}

// connectSupportAddress saves the email channel's support address and returns its inbound secret.
async function connectSupportAddress(page: Page, address: string): Promise<string> {
  await page.goto(`${origin}/settings/channels/type/email`);
  await page.getByRole('textbox', { name: 'Support address' }).fill(address);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await page.getByRole('button', { name: 'Create inbound secret' }).click();
  return await page.getByRole('textbox', { name: /Inbound secret/ }).inputValue();
}

// signInAtMicrosoft plays the person on the stand-in's sign-in page.
async function signInAtMicrosoft(page: Page, mailbox: string, kind: 'work' | 'personal' = 'work') {
  await expect(page.getByRole('heading', { name: 'Microsoft stand-in' })).toBeVisible();
  await page.getByRole('textbox', { name: 'Account' }).fill(mailbox);
  await page.getByRole('radio', { name: kind === 'work' ? 'Work or school account' : 'Personal account' }).check();
  await page.getByRole('button', { name: 'Accept' }).click();
}

test('an admin connects Microsoft 365, replies leave from the mailbox with threading, a revoked access says so, and disconnecting is honest', async ({ page }) => {
  const suffix = await signIn(page, 'E2E Microsoft');
  const support = `support-${suffix}@contoso.example`;
  const secret = await connectSupportAddress(page, support);
  await page.getByRole('tab', { name: 'Replies' }).click();
  const block = page.getByRole('region', { name: 'Send from your mailbox' });
  await expect(block.getByRole('button', { name: 'Connect Gmail' })).toHaveCount(0);
  await block.getByRole('button', { name: 'Connect Microsoft 365' }).click();
  // The stand-in's sign-in pre-fills the support address (login_hint).
  await expect(page.getByRole('textbox', { name: 'Account' })).toHaveValue(support);
  await signInAtMicrosoft(page, support);
  await expect(page.getByRole('status').filter({ hasText: 'Microsoft 365 is connected.' })).toBeVisible();
  await expect(block).toContainText(`Replies are sent through Microsoft 365 from ${support}`);

  const raw = [`From: Erin Customer <erin-${suffix}@customer.example>`, `To: ${support}`, 'Subject: E2E Microsoft thread',
    `Message-ID: <e2e-ms-${suffix}@customer.example>`, 'Content-Type: text/plain; charset=utf-8', '', 'Where is my order?'].join('\r\n');
  const inbound = await page.request.post(`${origin}/api/v1/channels/email/inbound`, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret }, data: Buffer.from(raw),
  });
  expect(inbound.status()).toBe(200);
  await page.goto(`${origin}/`);
  await page.getByRole('button', { name: /E2E Microsoft thread/ }).click();
  await page.getByLabel('Message body').last().fill('It ships today.');
  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect.poll(async () => (await sentThroughMicrosoft()).filter((sent) => sent.raw.includes('It ships today.')).length, { timeout: 30_000 }).toBe(1);
  const sent = (await sentThroughMicrosoft()).find((item) => item.raw.includes('It ships today.'))!;
  expect(sent.account).toBe(support);
  expect(sent.raw).toMatch(new RegExp(`^From: .*<${support.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}>`, 'm'));
  expect(sent.raw).toContain(`In-Reply-To: <e2e-ms-${suffix}@customer.example>`);
  await expect(page.getByText('Delivery failed', { exact: true })).toHaveCount(0);

  // The person removes Colliedesk in My Apps: the next reply fails visibly and the block turns into an alert.
  await standIn('revoke-all');
  await page.getByLabel('Message body').last().fill('One more thing.');
  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(page.getByText('Delivery failed', { exact: true })).toBeVisible({ timeout: 30_000 });
  await page.goto(`${origin}/settings/channels/type/email?tab=replies`);
  const alert = block.getByRole('alert');
  await expect(alert).toContainText('Microsoft 365 stopped sending');
  await expect(alert).toContainText('access was removed, the password was reset');
  await alert.getByRole('button', { name: 'Reconnect Microsoft 365' }).click();
  await signInAtMicrosoft(page, support);
  await expect(block).toContainText(`Replies are sent through Microsoft 365 from ${support}`);

  await block.getByRole('button', { name: 'Disconnect Microsoft 365' }).click();
  const question = block.getByRole('group', { name: 'Disconnect Microsoft 365?' });
  await expect(question.getByRole('link', { name: 'My Apps' })).toHaveAttribute('href', 'https://myapps.microsoft.com');
  await expect(question.getByRole('link', { name: 'Microsoft account permissions' })).toHaveAttribute('href', 'https://account.live.com/consent/Manage');
  await question.getByRole('button', { name: 'Yes, disconnect Microsoft 365' }).click();
  await expect(block.getByRole('button', { name: 'Connect Microsoft 365' })).toBeVisible();
});

test('a cancelled sign-in offers the admin approval link; an admin\'s approval returns to a page or to the channel', async ({ page, browser }) => {
  const suffix = await signIn(page, 'E2E Approval');
  await connectSupportAddress(page, `help-${suffix}@fabrikam.example`);
  await page.getByRole('tab', { name: 'Replies' }).click();
  const block = page.getByRole('region', { name: 'Send from your mailbox' });
  await block.getByRole('button', { name: 'Connect Microsoft 365' }).click();
  await page.getByRole('button', { name: 'Cancel' }).click();
  await expect(block.getByRole('alert')).toContainText('send your Microsoft 365 admin the approval link below');
  const approvalLink = await block.getByRole('textbox', { name: 'Approval link for your Microsoft 365 admin' }).inputValue();
  expect(approvalLink).toContain('/organizations/v2.0/adminconsent');

  // The IT admin has no Colliedesk session: approving ends on a page of ours that says what happens next.
  const itAdmin = await browser.newContext();
  const itPage = await itAdmin.newPage();
  await itPage.goto(approvalLink);
  await itPage.getByRole('button', { name: 'Accept' }).click();
  await expect(itPage.getByRole('heading', { name: 'Colliedesk is approved' })).toBeVisible();
  await expect(itPage.getByText('Tell your Colliedesk admin to select Connect Microsoft 365 again.')).toBeVisible();
  await itAdmin.close();

  // The Colliedesk admin approving in their own browser returns to the channel.
  await page.goto(approvalLink);
  await page.getByRole('button', { name: 'Accept' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Your Microsoft 365 admin approved Colliedesk.' })).toBeVisible();
});

test('a personal Outlook.com mailbox connects by its sign-in name', async ({ page }) => {
  const suffix = await signIn(page, 'E2E Outlook');
  const support = `desk-${suffix}@outlook.com`;
  await connectSupportAddress(page, support);
  await page.getByRole('tab', { name: 'Replies' }).click();
  const block = page.getByRole('region', { name: 'Send from your mailbox' });
  await block.getByRole('button', { name: 'Connect Microsoft 365' }).click();
  await signInAtMicrosoft(page, support, 'personal');
  await expect(block).toContainText(`Replies are sent through Microsoft 365 from ${support}`);
});
