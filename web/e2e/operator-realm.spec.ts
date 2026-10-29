import type { ChildProcess } from 'node:child_process';
import { expect, test } from '@playwright/test';
import { databaseURL, mustColliedesk, psql, startServer, stopServer, uniqueSuffix, withDatabase } from './support/cli';
import { ports } from './support/checkout';

// The console on its own host (OPERATOR_ORIGIN): cookies do not separate by port, so the test uses another host
// name on the same port (Chromium resolves *.localhost to loopback).
const port = ports.operatorHost;
const appOrigin = `http://localhost:${port}`;
const consoleOrigin = `http://ops.localhost:${port}`;
const database = `colliedesk_operator_host_${process.pid}`;
const hostURL = withDatabase(databaseURL, database);
let server: ChildProcess | undefined;

test.beforeAll(async () => {
  psql(`DROP DATABASE IF EXISTS ${database}`, withDatabase(databaseURL, 'postgres'));
  psql(`CREATE DATABASE ${database}`, withDatabase(databaseURL, 'postgres'));
  expect(mustColliedesk(['migrate'], { databaseURL: hostURL, origin: appOrigin })).toBe('migrations applied');
  server = await startServer(port, { DATABASE_URL: hostURL, APP_ORIGIN: appOrigin, OPERATOR_ORIGIN: consoleOrigin });
});

test.afterAll(async () => {
  await stopServer(server);
  psql(`DROP DATABASE IF EXISTS ${database} WITH (FORCE)`, withDatabase(databaseURL, 'postgres'));
});

test('the console on its own host: its cookie never reaches the app host, and each host serves only its own pages', async ({ page }) => {
  const link = mustColliedesk(['operator', 'pair', '--email', `ops-${uniqueSuffix()}@example.com`], { databaseURL: hostURL, origin: appOrigin, env: { OPERATOR_ORIGIN: consoleOrigin } });
  expect(link.startsWith(`${consoleOrigin}/sign-in?token=`)).toBe(true);
  await page.goto(link);
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByRole('button', { name: 'Go to the overview' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
  const cookies = await page.context().cookies(appOrigin);
  expect(cookies.filter((cookie) => cookie.name.includes('operator'))).toEqual([]);
  expect((await page.request.get(`${appOrigin}/api/v1/operator/auth/me`)).status()).toBe(404);
  await page.goto(`${appOrigin}/operator/`);
  await expect(page.getByRole('heading', { name: 'The operator console has its own address' })).toBeVisible();
  expect((await page.request.get(`${consoleOrigin}/inbox`)).status()).toBe(404);
});
