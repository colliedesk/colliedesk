import { readFileSync } from 'node:fs';
import { expect, test } from '@playwright/test';
import type { APIResponse, Page } from '@playwright/test';
import { appNavigationWith } from './support/browser';
import { psql, servers, uniqueSuffix } from './support/cli';
import { IdentityProvider } from './support/identity-provider';

// Journey 9: a new customer on the hosted cloud (docs/user/sign-in.md "Create a workspace",
// docs/user/billing.md). The identity provider is the only local stand-in; Clerk's own widget is not
// part of this journey, so the verified token it would hand over is signed by the stand-in.
const baseURL = servers.cloud;
const provider = new IdentityProvider();

test.beforeAll(async () => { await provider.start(); });
test.afterAll(async () => { await provider.stop(); });

// Every sign-up comes from its own client address (the server trusts X-Forwarded-For from this host),
// so repeated runs against one database never meet the three-per-address limit.
function newClientAddress() {
  return `10.${Math.floor(Math.random() * 256)}.${Math.floor(Math.random() * 256)}.${1 + Math.floor(Math.random() * 254)}`;
}

function signUp(page: Page, email: string, workspaceName: string): Promise<APIResponse> {
  return page.request.post(`${baseURL}/api/v1/auth/cloud/signup`, {
    headers: { Origin: baseURL, Authorization: `Bearer ${provider.token({ email })}`, 'X-Forwarded-For': newClientAddress() },
    data: { workspace_name: workspaceName },
  });
}

test('cloud signup: a verified identity starts a trial, the same email cannot start a second one, the expired trial is locked and still exports', { tag: '@onboarding' }, async ({ page }) => {
  const suffix = uniqueSuffix();
  const email = `owner-${suffix}@example.com`;
  const name = `Signup Desk ${suffix}`;

  const config = await (await page.request.get(`${baseURL}/api/v1/config`)).json() as { signup: { enabled: boolean } };
  expect(config.signup).toEqual({ enabled: true });

  // A sign-up needs a verified identity: no token and a token for another audience are refused.
  const unauthenticated = await page.request.post(`${baseURL}/api/v1/auth/cloud/signup`, { headers: { Origin: baseURL }, data: { workspace_name: name } });
  expect(unauthenticated.status()).toBe(401);
  const unverified = await page.request.post(`${baseURL}/api/v1/auth/cloud/signup`, {
    headers: { Origin: baseURL, Authorization: `Bearer ${provider.token({ email, email_verified: false })}`, 'X-Forwarded-For': newClientAddress() },
    data: { workspace_name: name },
  });
  expect(unverified.status()).toBe(401);

  const created = await signUp(page, email, name);
  expect(created.status()).toBe(201);
  const body = await created.json() as { workspace: { slug: string; name: string }; agent: { email: string; role: string } };
  expect(body.agent).toMatchObject({ email, role: 'admin' });

  // The session cookie from the sign-up opens the new workspace with its trial banner.
  await page.goto(baseURL);
  await expect(appNavigationWith(page, name)).toHaveCount(1);
  await expect(page.getByRole('region', { name: 'Plan notices' })).toContainText('Trial: 14 days left');
  await expect(page.getByRole('button', { name: 'Choose a plan' })).toBeVisible();

  // One trial per mailbox, however it is written.
  const again = await signUp(page, email.toUpperCase(), `Another ${name}`);
  expect(again.status()).toBe(409);
  expect(await again.text()).toContain('trial_already_used');

  // The trial ends: the whole workspace becomes the locked screen. Only the clock moves; the
  // server derives the state on every request.
  psql(`UPDATE workspaces SET trial_ends_at = now() - interval '1 hour' WHERE slug = '${body.workspace.slug}'`);
  await page.goto(baseURL);
  await expect(page.getByRole('heading', { level: 1, name: 'Your free trial has ended' })).toBeVisible();
  await expect(appNavigationWith(page, name)).toHaveCount(1);
  await expect(page.getByRole('button', { name: /Team, \$49 per month/ })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Message body' })).toHaveCount(0);

  // Locked is not lost: the admin still takes everything out as one ZIP with its manifest.
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Export all data' }).click();
  const file = await download;
  expect(file.suggestedFilename()).toMatch(/^colliedesk-.*\.zip$/);
  const archive = readFileSync(await file.path());
  expect(archive.subarray(0, 2).toString()).toBe('PK');
  expect(archive.includes(Buffer.from('manifest.json'))).toBe(true);
  await expect(page.getByRole('status')).toContainText('Export downloaded');

  // Signing out ends the session.
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByRole('alert')).toContainText('You are signed out');
  expect((await page.request.get(`${baseURL}/api/v1/auth/me`)).status()).toBe(401);
});
