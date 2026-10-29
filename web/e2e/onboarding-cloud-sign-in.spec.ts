import { expect, test } from '@playwright/test';
import { appNavigationWith } from './support/browser';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { IdentityProvider, identity, otherKey } from './support/identity-provider';

// Journey 5: hosted cloud sign-in (docs/user/sign-in.md "Cloud sign-in") on a server configured
// with the same cloud_auth.* settings an operator saves (collie config). Only the identity provider is local.
const baseURL = servers.cloud;
const exchangeURL = `${baseURL}/api/v1/auth/cloud/exchange`;
const provider = new IdentityProvider();

test.beforeAll(async () => { await provider.start(); });
test.afterAll(async () => { await provider.stop(); });

test('cloud sign-in: a verified identity of an existing agent gets a Colliedesk session; every other token is refused', { tag: '@onboarding' }, async ({ page }) => {
  const suffix = uniqueSuffix();
  const email = `cloud-${suffix}@example.com`;
  createWorkspace(`Cloud Desk ${suffix}`, email, baseURL);

  const config = await (await page.request.get(`${baseURL}/api/v1/config`)).json() as { cloud_sign_in: { enabled: boolean; publishable_key: string } };
  expect(config.cloud_sign_in).toEqual({ enabled: true, publishable_key: identity.publishableKey });

  const exchange = (token: string, origin: string = baseURL) => page.request.post(exchangeURL, {
    headers: { Origin: origin, Authorization: `Bearer ${token}` }, data: {},
  });
  const refusals: Array<[string, string, number]> = [
    ['unknown email', provider.token({ email: `stranger-${suffix}@example.com` }), 403],
    ['wrong issuer', provider.token({ email, iss: 'https://attacker.example' }), 401],
    ['wrong authorized party', provider.token({ email, azp: 'https://other-app.example' }), 401],
    ['expired', provider.token({ email, exp: Math.floor(Date.now() / 1000) - 3600 }), 401],
    ['not yet valid', provider.token({ email, nbf: Math.floor(Date.now() / 1000) + 3600 }), 401],
    ['email not verified', provider.token({ email, email_verified: false }), 401],
    ['signed by another key', provider.token({ email }, { key: otherKey() }), 401],
    ['unknown key id', provider.token({ email }, { key: otherKey(), kid: 'rotated-away' }), 401],
  ];
  for (const [name, token, status] of refusals) {
    const response = await exchange(token);
    expect(response.status(), name).toBe(status);
    expect(response.headers()['set-cookie'], name).toBeUndefined();
  }
  expect((await exchange(provider.token({ email }), 'https://evil.example')).status(), 'foreign origin').toBe(403);
  expect((await page.request.get(`${baseURL}/api/v1/auth/me`)).status()).toBe(401);

  // A visitor whose cloud session ended lands on the sign-in page, not on a self-host pairing hint,
  // from the inbox address and from one that matches no page alike.
  const visitor = await page.context().browser()!.newPage();
  for (const path of ['/', '/no-such-route']) {
    await visitor.goto(`${baseURL}${path}`);
    await expect(visitor).toHaveURL(`${baseURL}/sign-in?signed_out=true`);
    await expect(visitor.getByRole('alert')).toContainText('You are signed out');
    await expect(visitor.getByRole('heading', { level: 1, name: 'Sign in to Colliedesk' })).toBeVisible();
    await expect(visitor.getByText(/collie pair/)).toHaveCount(0);
  }
  await visitor.close();

  const accepted = await exchange(provider.token({ email: email.toUpperCase() }));
  expect(accepted.status()).toBe(200);
  const body = await accepted.json() as { token?: string; agent: { email: string; role: string } };
  expect(body.token ?? '').toBe('');
  expect(body.agent).toMatchObject({ email, role: 'admin' });

  // The Colliedesk session is an ordinary browser session: the inbox opens and the device is listed.
  await page.goto(baseURL);
  await expect(appNavigationWith(page, `Cloud Desk ${suffix}`)).toHaveCount(1);
  await page.goto(`${baseURL}/settings`);
  // The cloud session carries this browser's user agent, so Settings names it and marks it as this device.
  await expect(page.getByRole('region', { name: 'Sessions' })).toContainText('This device');
});
