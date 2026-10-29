import type { ChildProcess } from 'node:child_process';
import { expect, test } from '@playwright/test';
import { databaseURL, mustColliedesk, psql, startServer, stopServer, uniqueSuffix, withDatabase } from './support/cli';
import { ports } from './support/checkout';
import { mailpit, waitForMail } from './support/mailpit';

// The operator console on a server of its own (docs/self-host/operator-console.md): its own database, so its
// configuration changes touch no other journey.
const port = ports.operator;
const origin = `http://localhost:${port}`;
const database = `colliedesk_operator_${process.pid}`;
const operatorURL = withDatabase(databaseURL, database);
const ring = 'k1:ZTJlLW9wZXJhdG9yLWNvbnNvbGUta2V5LTMyLWJ5dGU=';
let server: ChildProcess | undefined;

test.beforeAll(async () => {
  psql(`DROP DATABASE IF EXISTS ${database}`, withDatabase(databaseURL, 'postgres'));
  psql(`CREATE DATABASE ${database}`, withDatabase(databaseURL, 'postgres'));
  expect(mustColliedesk(['migrate'], { databaseURL: operatorURL, origin })).toBe('migrations applied');
  server = await startServer(port, { DATABASE_URL: operatorURL, APP_ORIGIN: origin, MAILBOX_TOKEN_KEYS: ring });
});

test.afterAll(async () => {
  await stopServer(server);
  psql(`DROP DATABASE IF EXISTS ${database} WITH (FORCE)`, withDatabase(databaseURL, 'postgres'));
});

test('operator console: the installer\'s operator link signs in, the operator sets SMTP and sends a test, the change applies without a restart, history restores the previous version', { tag: '@onboarding' }, async ({ page }) => {
  const email = `ops-${uniqueSuffix()}@example.com`;
  // What install.sh saves before the server starts (the reverse proxy it trusts): the first version.
  mustColliedesk(['config', 'set', 'trusted_proxies=127.0.0.0/8,::1/128,172.16.0.0/12', '--yes'], { databaseURL: operatorURL, origin, env: { MAILBOX_TOKEN_KEYS: ring } });
  // What install.sh runs after the workspace: the same command prints the operator's link.
  const link = mustColliedesk(['operator', 'pair', '--email', email], { databaseURL: operatorURL, origin, env: { MAILBOX_TOKEN_KEYS: ring } });
  expect(link).toMatch(new RegExp(`^${origin}/operator/sign-in\\?token=[A-Za-z0-9_-]{32,}$`));

  await page.goto(link);
  // The token leaves the address before anything else, and nothing is spent until Continue.
  await expect(page).toHaveURL(`${origin}/operator/sign-in`);
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByRole('button', { name: 'Go to the overview' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
  // A fresh server: the console shares the app's address, and says so.
  await expect(page.getByText(/The console shares the workspace app's address/)).toBeVisible();

  await page.getByRole('link', { name: 'Email', exact: true }).click();
  await page.getByLabel('SMTP server').fill('127.0.0.1');
  await page.getByLabel('Port').fill(mailpit.smtpPort);
  await page.getByRole('switch', { name: 'Use STARTTLS' }).uncheck();
  await expect(page.getByText(/3 unsaved changes/)).toBeVisible();
  await page.getByRole('button', { name: `Send a test email to ${email}` }).click();
  await expect(page.getByRole('status').filter({ hasText: `Sent a test email to ${email}. Nothing was saved.` })).toBeVisible();
  const test = await waitForMail(email, 30_000, 'This is a test from the operator console');
  expect(test.subject).toBe('Colliedesk test email');

  await page.getByRole('button', { name: 'Review and save' }).click();
  const review = page.getByRole('dialog', { name: 'Review 3 changes' });
  await expect(review.getByText('Will be saved as v')).toBeVisible();
  await review.getByRole('button', { name: 'Save and apply' }).click();
  await expect(page.getByRole('status').filter({ hasText: /Saved v\d+\. Every server applies it within a few seconds\./ })).toBeVisible();
  // The running server picks the version up by itself: the band says every process runs it.
  await expect(page.getByRole('banner').getByRole('status')).toHaveText(/Running v(\d+) · 1 of 1 processes/, { timeout: 15_000 });
  const saved = Number((await page.getByRole('banner').getByRole('status').textContent())?.match(/v(\d+)/)?.[1]);

  await page.getByRole('link', { name: 'History', exact: true }).click();
  await page.getByRole('button', { name: new RegExp(`^v${saved - 1}`) }).click();
  await page.getByRole('button', { name: `Restore v${saved - 1}` }).click();
  await page.getByRole('dialog').getByRole('button', { name: `Restore v${saved - 1}` }).click();
  await expect(page.getByRole('banner').getByRole('status')).toHaveText(new RegExp(`Running v${saved + 1} · 1 of 1 processes`), { timeout: 15_000 });
  const config = await page.request.get(`${origin}/api/v1/operator/config`);
  const smtpHost = ((await config.json()) as { settings: Array<{ key: string; value: string | null }> }).settings.find((setting) => setting.key === 'smtp.host');
  expect(smtpHost?.value ?? null).toBeNull();
});

test('a link opened twice explains itself; the console shows no screen without a session', async ({ page }) => {
  const email = `ops-twice-${uniqueSuffix()}@example.com`;
  const link = mustColliedesk(['operator', 'pair', '--email', email], { databaseURL: operatorURL, origin, env: { MAILBOX_TOKEN_KEYS: ring } });
  await page.goto(link);
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByRole('button', { name: 'Go to the overview' }).click();
  await page.context().clearCookies();
  await page.goto(link);
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByRole('heading', { name: 'This link was used already' })).toBeVisible();
  await page.goto(`${origin}/operator/`);
  await expect(page.getByRole('heading', { name: 'Your session ended' })).toBeVisible();
});
