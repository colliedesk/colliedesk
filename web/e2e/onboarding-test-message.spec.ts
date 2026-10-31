import { expect, test } from '@playwright/test';
import { servers, uniqueSuffix } from './support/cli';
import { IdentityProvider } from './support/identity-provider';
import { waitForMail } from './support/mailpit';
import { signUp } from './support/signup';

// A new team's first success without a forwarder or a terminal (docs/user/email-channel.md, "Send
// yourself a test message"): on the hosted cloud, a fresh trial connects its support address, sends
// itself a test message from the empty inbox, answers it on the trial and gets the reply by SMTP.
const baseURL = servers.cloud;
const provider = new IdentityProvider();

test.beforeAll(async () => { await provider.start(); });
test.afterAll(async () => { await provider.stop(); });

test('test message: a new cloud workspace sends itself a test message from the empty inbox, answers it on the trial, and the reply is delivered', { tag: '@onboarding' }, async ({ page }) => {
  const suffix = uniqueSuffix();
  const adminEmail = `owner-${suffix}@acme.example`;
  const support = `support-${suffix}@acme.example`;
  expect((await signUp(page, provider, adminEmail, `Test Message Desk ${suffix}`)).status()).toBe(201);
  await page.goto(baseURL);
  await expect(page.getByRole('region', { name: 'Plan notices' })).toContainText(/Trial ends [A-Z][a-z]{2} \d{1,2}/);

  await page.getByRole('region', { name: 'Finish setting up' }).getByRole('link', { name: /Receive customer email/ }).click();
  await page.getByRole('textbox', { name: 'Support address' }).fill(support);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await expect(page.getByRole('group', { name: 'Forward its mail to Colliedesk' })).toContainText('Step 2 of 4');

  // Back on the empty inbox, one click brings a conversation from the agent's own address.
  await page.getByRole('link', { name: 'Inbox' }).click();
  await page.getByRole('region', { name: 'Get started' }).getByRole('button', { name: 'Send yourself a test message' }).click();
  await expect(page).toHaveURL(/\/conversations\/[0-9a-f-]{36}$/);
  await expect(page.getByText('This is a test message you sent yourself from Colliedesk.')).toBeVisible();

  // The trial lets this reply go: its only recipient is the workspace's own, verified agent.
  const reply = `Answering my own test (${suffix}).`;
  await page.getByLabel('Message body').last().fill(reply);
  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(page.getByText('Sent', { exact: true })).toBeVisible({ timeout: 30_000 });

  const delivered = await waitForMail(adminEmail, 30_000, suffix);
  expect(delivered.from).toBe(support);
  expect(delivered.subject).toBe('Re: Test message from Colliedesk');
  expect(delivered.text).toContain(reply);
});

// The Connection tab offers the same test message before any inbound secret exists: like the inbox's action,
// a press opens the conversation it started, and presses fired in one burst open a single conversation.
test('the Connection tab sends one test message before an inbound secret exists and opens its conversation', async ({ page }) => {
  const suffix = uniqueSuffix();
  const adminEmail = `owner-${suffix}@acme.example`;
  expect((await signUp(page, provider, adminEmail, `Connection Test ${suffix}`)).status()).toBe(201);
  await page.goto(baseURL);
  await page.getByRole('region', { name: 'Finish setting up' }).getByRole('link', { name: /Receive customer email/ }).click();
  await page.getByRole('textbox', { name: 'Support address' }).fill(`support-${suffix}@acme.example`);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await page.getByRole('link', { name: 'Manage this address' }).click();
  await expect(page.getByRole('tab', { name: 'Connection' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Create inbound secret' })).toBeVisible();

  const send = page.getByRole('button', { name: 'Send yourself a test message' });
  await send.evaluate((button: HTMLButtonElement) => { button.click(); button.click(); button.click(); });
  await expect(page).toHaveURL(/\/conversations\/[0-9a-f-]{36}$/);
  await expect(page.getByText('This is a test message you sent yourself from Colliedesk.')).toBeVisible();
  const list = page.getByRole('region', { name: 'Conversation list' });
  await expect(list.getByText('Test message from Colliedesk')).toHaveCount(1);
});
