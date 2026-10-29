import type { ChildProcess } from 'node:child_process';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { colliedesk, databaseURL, mustColliedesk, psql, startServer, stopServer, uniqueSuffix, withDatabase } from './support/cli';
import { ports } from './support/checkout';
import { redeemLink } from './support/browser';
import { microsoftStandIn } from './support/microsoft';

// Feature switches (docs/self-host/operator-console.md#features) on a server and database of their own: a
// switch changes what every workspace of the server sees.
const port = ports.features;
const origin = `http://localhost:${port}`;
const database = `colliedesk_e2e_features_${process.pid}`;
const featuresURL = withDatabase(databaseURL, database);
const ring = 'k1:ZTJlLWZlYXR1cmUtc3dpdGNoZXMta2V5LTMyLWJ5dGU=';
let server: ChildProcess | undefined;

test.describe.configure({ mode: 'default' });

test.beforeAll(async () => {
  psql(`DROP DATABASE IF EXISTS ${database}`, withDatabase(databaseURL, 'postgres'));
  psql(`CREATE DATABASE ${database}`, withDatabase(databaseURL, 'postgres'));
  expect(mustColliedesk(['migrate'], { databaseURL: featuresURL, origin })).toBe('migrations applied');
  // Microsoft 365 is set up (imported from the environment at the first start); Gmail is not.
  server = await startServer(port, { DATABASE_URL: featuresURL, APP_ORIGIN: origin, MAILBOX_TOKEN_KEYS: ring, ...microsoftStandIn.env() }, 'e2e');
});

test.afterAll(async () => {
  await stopServer(server);
  psql(`DROP DATABASE IF EXISTS ${database} WITH (FORCE)`, withDatabase(databaseURL, 'postgres'));
});

let channelPage = '';

async function repliesBlock(page: Page) {
  await page.goto(channelPage);
  await page.getByRole('tab', { name: 'Replies' }).click();
  return page.getByRole('region', { name: 'Send from your mailbox' });
}

async function saveSwitch(page: Page, name: string, on: boolean) {
  const control = page.getByRole('switch', { name });
  await (on ? control.check() : control.uncheck());
  await page.getByRole('button', { name: 'Review and save' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Save and apply' }).click();
  await expect(page.getByRole('status').filter({ hasText: /Saved v(\d+)\./ })).toBeVisible();
  const saved = (await page.getByRole('status').filter({ hasText: /Saved v(\d+)\./ }).textContent())?.match(/v(\d+)/)?.[1];
  await expect(page.getByRole('banner').getByRole('status')).toHaveText(new RegExp(`Running v${saved} · 1 of 1 processes`), { timeout: 15_000 });
}

test('feature switches: a feature missing settings cannot be turned on; switched off it leaves the app, nothing is removed, and on again it is back', async ({ page }) => {
  const suffix = uniqueSuffix();
  const created = colliedesk(['create-workspace', '--name', `E2E Features ${suffix}`, '--admin-email', `admin-${suffix}@example.com`], { databaseURL: featuresURL, origin });
  expect(created.status, created.stderr).toBe(0);
  await redeemLink(page, created.stdout.trim());
  await page.goto(`${origin}/settings/channels/type/email`);
  await page.getByRole('textbox', { name: 'Support address' }).fill(`support-${suffix}@contoso.example`);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await page.getByRole('button', { name: 'Create inbound secret' }).click();
  await page.getByRole('link', { name: 'Manage this address' }).click();
  await expect(page).toHaveURL(/\/settings\/channels\/[0-9a-f-]+/);
  channelPage = page.url();
  let block = await repliesBlock(page);
  await expect(block.getByRole('button', { name: 'Connect Microsoft 365' })).toBeVisible();

  const link = mustColliedesk(['operator', 'pair', '--email', `ops-${suffix}@example.com`], { databaseURL: featuresURL, origin, env: { MAILBOX_TOKEN_KEYS: ring } });
  await page.goto(link);
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByRole('button', { name: 'Go to the overview' }).click();
  await page.getByRole('link', { name: 'Features', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Features' })).toBeVisible();
  // Gmail has no settings here: its switch is greyed out and says what it needs, each a link to its page.
  const gmail = page.getByRole('switch', { name: /Gmail mailboxes/ });
  await expect(gmail).toBeDisabled();
  await expect(gmail).not.toBeChecked();
  await expect(page.getByRole('link', { name: 'Google client ID' })).toBeVisible();
  await expect(page.getByRole('switch', { name: /Microsoft 365 mailboxes/ })).toBeChecked();

  await saveSwitch(page, 'Microsoft 365 mailboxes', false);
  block = await repliesBlock(page);
  await expect(block.getByRole('button', { name: 'Connect Microsoft 365' })).toHaveCount(0);
  await expect(block.getByText('Microsoft 365 is not turned on on this server.')).toBeVisible();
  await expect(block).not.toContainText('MICROSOFT_OAUTH');
  const config = await page.request.get(`${origin}/api/v1/config`);
  expect(((await config.json()) as { features: Record<string, { enabled: boolean }> }).features.microsoft.enabled).toBe(false);

  await page.goto(`${origin}/operator/config/features`);
  await saveSwitch(page, 'Microsoft 365 mailboxes', true);
  block = await repliesBlock(page);
  await expect(block.getByRole('button', { name: 'Connect Microsoft 365' })).toBeVisible();
});
