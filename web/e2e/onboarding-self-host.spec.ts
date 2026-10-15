import type { ChildProcess } from 'node:child_process';
import { expect, test } from '@playwright/test';
import { colliedesk, databaseURL, mustColliedesk, psql, startServer, stopServer, uniqueSuffix, withDatabase } from './support/cli';
import { ports } from './support/checkout';
import { appNavigationWith, expectReadablePage, redeemLink } from './support/browser';

// Journey 1: a self-hoster follows docs/user/install.md "From a clone" on an empty database.
const port = ports.fresh;
const origin = `http://localhost:${port}`;
const database = `colliedesk_onboard_fresh_${process.pid}`;
const freshURL = withDatabase(databaseURL, database);
let server: ChildProcess | undefined;

test.beforeAll(async () => {
  psql(`DROP DATABASE IF EXISTS ${database}`, withDatabase(databaseURL, 'postgres'));
  psql(`CREATE DATABASE ${database}`, withDatabase(databaseURL, 'postgres'));
});

test.afterAll(async () => {
  await stopServer(server);
  psql(`DROP DATABASE IF EXISTS ${database} WITH (FORCE)`, withDatabase(databaseURL, 'postgres'));
});

test('self-host first run: fresh database, create-workspace, pairing link, empty inbox that explains the next step', { tag: '@onboarding' }, async ({ page }) => {
  expect(mustColliedesk(['migrate'], { databaseURL: freshURL, origin })).toBe('migrations applied');
  server = await startServer(port, { DATABASE_URL: freshURL, APP_ORIGIN: origin });

  const suffix = uniqueSuffix();
  const created = colliedesk(['create-workspace', '--name', 'Acme Support', '--admin-email', `owner-${suffix}@example.com`],
    { databaseURL: freshURL, origin });
  expect(created.status, created.stderr).toBe(0);
  // stdout carries only the link (install.sh captures it); the next-step hint goes to stderr.
  const pairingURL = created.stdout.trim();
  expect(pairingURL).toMatch(new RegExp(`^${origin}/redeem\\?token=[A-Za-z0-9_-]{32,}$`));

  await redeemLink(page, pairingURL);
  await expect(appNavigationWith(page, 'Acme Support')).toHaveCount(1);
  await expect(appNavigationWith(page, `owner-${suffix}@example.com`)).toHaveCount(1);

  const empty = page.getByRole('region', { name: 'Get started' });
  await expect(empty).toContainText('No conversations yet');
  await expect(empty.getByRole('link', { name: 'Connect your support email' })).toHaveAttribute('href', '/settings');
  await expect(empty.getByRole('link', { name: 'Import from Freshdesk' })).toHaveAttribute('href', /import-freshdesk/);
  await expect(empty.getByRole('link', { name: 'Invite your team' })).toHaveAttribute('href', '/settings');
  await expectReadablePage(page);

  // A failed list (server error) is reported with a retry, not as "No conversations yet".
  await page.route('**/api/v1/conversations?*', (route) => route.fulfill({ status: 500, body: '{}' }));
  await page.reload();
  await expect(page.getByRole('alert')).toContainText('Conversations could not be loaded.');
  await expect(page.getByRole('region', { name: 'Get started' })).toHaveCount(0);
  await page.unroute('**/api/v1/conversations?*');
  await page.getByRole('button', { name: 'Try again' }).click();
  await expect(empty).toContainText('No conversations yet');

  // Next steps open inside the app (no full page load).
  await page.evaluate(() => { (window as unknown as { stillHere: boolean }).stillHere = true; });
  await empty.getByRole('link', { name: 'Connect your support email' }).click();
  await expect(page.getByRole('heading', { name: 'Email channel' })).toBeVisible();
  expect(await page.evaluate(() => (window as unknown as { stillHere?: boolean }).stillHere)).toBe(true);

  // The docs: "The link works once". A second open of the same link must not sign anyone in.
  const reuse = await page.request.post(`${origin}/api/v1/auth/redeem`, { data: { token: new URL(pairingURL).searchParams.get('token') } });
  expect(reuse.status()).toBe(401);

  // The docs: "If it expires, print a new one" with `pair --workspace <slug>`. The slug must be told
  // to the self-hoster at creation time; they cannot sign in to look it up once the link expired.
  const hint = created.stderr.match(/collie pair --workspace (\S+) --email (\S+)/);
  expect(hint, `create-workspace stderr: ${created.stderr}`).not.toBeNull();
  expect(hint?.[2]).toBe(`owner-${suffix}@example.com`);
  const repaired = mustColliedesk(['pair', '--workspace', hint?.[1] ?? '', '--email', hint?.[2] ?? ''], { databaseURL: freshURL, origin });
  expect(repaired).toMatch(new RegExp(`^${origin}/redeem\\?token=`));
});
