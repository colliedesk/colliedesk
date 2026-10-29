import type { ChildProcess } from 'node:child_process';
import { expect, test } from '@playwright/test';
import { databaseURL, mustColliedesk, psql, startServer, stopServer, uniqueSuffix, withDatabase } from './support/cli';
import { ports } from './support/checkout';

// D12 against a real server: a change of trusted proxies is tried, reverts by itself when nobody confirms it,
// and stays when confirmed. The e2e build shortens a trial to a few seconds (COLLIEDESK_E2E_TRIAL_SECONDS).
const port = ports.operatorTrial;
const origin = `http://localhost:${port}`;
const database = `colliedesk_operator_trial_${process.pid}`;
const trialURL = withDatabase(databaseURL, database);
let server: ChildProcess | undefined;

test.beforeAll(async () => {
  psql(`DROP DATABASE IF EXISTS ${database}`, withDatabase(databaseURL, 'postgres'));
  psql(`CREATE DATABASE ${database}`, withDatabase(databaseURL, 'postgres'));
  expect(mustColliedesk(['migrate'], { databaseURL: trialURL, origin })).toBe('migrations applied');
  mustColliedesk(['config', 'set', 'trusted_proxies=127.0.0.0/8', '--yes'], { databaseURL: trialURL, origin });
  server = await startServer(port, { DATABASE_URL: trialURL, APP_ORIGIN: origin, COLLIEDESK_E2E_TRIAL_SECONDS: '6' }, 'e2e');
});

test.afterAll(async () => {
  await stopServer(server);
  psql(`DROP DATABASE IF EXISTS ${database} WITH (FORCE)`, withDatabase(databaseURL, 'postgres'));
});

async function tryProxies(page: import('@playwright/test').Page, value: string) {
  await page.getByRole('link', { name: 'Security & network', exact: true }).click();
  await page.getByLabel('Trusted proxies').fill(value);
  await page.getByRole('button', { name: 'Review and save' }).click();
  const review = page.getByRole('dialog');
  await expect(review.getByText(/Under this list your own address reads as/)).toBeVisible();
  await expect(review.getByText(/reverts by itself/)).toBeVisible();
  await review.getByRole('button', { name: 'Save and apply' }).click();
  await expect(page.getByRole('status').filter({ hasText: /as a trial/ })).toBeVisible();
}

test('a trial change reverts by itself unless confirmed, and stays once confirmed', async ({ page }) => {
  test.setTimeout(150_000);
  const link = mustColliedesk(['operator', 'pair', '--email', `ops-${uniqueSuffix()}@example.com`], { databaseURL: trialURL, origin });
  await page.goto(link);
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByRole('button', { name: 'Go to the overview' }).click();

  await tryProxies(page, '10.0.0.0/8');
  const strip = page.getByRole('region', { name: 'Trial in progress' });
  await expect(strip).toBeVisible({ timeout: 15_000 });
  await expect(strip.getByRole('timer')).toHaveText(/^0:0\d$/);
  // Nobody confirms: the server reverts it, and the band follows within its next look at the status.
  await expect(strip).toBeHidden({ timeout: 45_000 });
  expect(mustColliedesk(['config', 'get', 'trusted_proxies'], { databaseURL: trialURL, origin })).toBe('trusted_proxies=127.0.0.0/8');
  await page.getByRole('link', { name: 'History', exact: true }).click();
  await expect(page.getByText(/trial-auto-revert/).first()).toBeVisible();

  // Confirmed from the console, it stays.
  await page.reload();
  await tryProxies(page, '10.1.0.0/16');
  await expect(strip).toBeVisible({ timeout: 15_000 });
  await strip.getByRole('button', { name: /^Confirm v\d+$/ }).click();
  await page.getByRole('dialog').getByRole('button', { name: /^Confirm v\d+$/ }).click();
  await expect(strip).toBeHidden();
  await page.waitForTimeout(8_000);
  expect(mustColliedesk(['config', 'get', 'trusted_proxies'], { databaseURL: trialURL, origin })).toBe('trusted_proxies=10.1.0.0/16');
});
