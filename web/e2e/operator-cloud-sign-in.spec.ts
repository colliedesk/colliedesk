import { createHmac } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { mustColliedesk, servers, uniqueSuffix } from './support/cli';
import { IdentityProvider } from './support/identity-provider';

// The operator console's cloud sign-in (docs/self-host/operator-console.md) on the cloud server, with the real
// server, CLI and PostgreSQL; only the identity provider is local. That server runs the console at its own
// address (no OPERATOR_ORIGIN), so the console's origin is the app's and so is the token's authorized party.
const baseURL = servers.cloud;
const ring = { MAILBOX_TOKEN_KEYS: 'c1:ZTJlLWNsb3VkLWltcG9ydC1rZXktMzItYnl0ZXMhISE=' };
const provider = new IdentityProvider();

test.beforeAll(async () => { await provider.start(); });
test.afterAll(async () => { await provider.stop(); });

// totp is the authenticator app: RFC 6238, 30 seconds, 6 digits, from the key the console shows (base32).
function totp(key: string, at = Date.now()): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const char of key.replace(/\s/g, '').toUpperCase()) bits += alphabet.indexOf(char).toString(2).padStart(5, '0');
  const secret = Buffer.from((bits.match(/.{8}/g) ?? []).map((byte) => parseInt(byte, 2)));
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 30_000)));
  const mac = createHmac('sha1', secret).update(counter).digest();
  const offset = mac[mac.length - 1] & 0xf;
  return String((mac.readUInt32BE(offset) & 0x7fffffff) % 1_000_000).padStart(6, '0');
}

test('operator cloud sign-in: an operator added without a link signs in with their email and the authenticator; everyone else is refused alike', async ({ page }) => {
  const suffix = uniqueSuffix();
  const email = `ops-cloud-${suffix}@example.com`;
  const gone = `ops-gone-${suffix}@example.com`;
  expect(mustColliedesk(['operator', 'add', '--email', email, '--by', 'e2e'], { origin: baseURL, env: ring })).toContain(`added ${email}`);
  mustColliedesk(['operator', 'add', '--email', gone, '--by', 'e2e'], { origin: baseURL, env: ring });
  mustColliedesk(['operator', 'revoke', gone, '--by', 'e2e'], { origin: baseURL, env: ring });

  const signIn = (token: string, origin: string = baseURL) => page.request.post(`${baseURL}/api/v1/operator/auth/cloud`, {
    headers: { Origin: origin, Authorization: `Bearer ${token}` },
  });
  const refusals: Array<[string, string]> = [
    ['not an operator', provider.token({ email: `stranger-${suffix}@example.com` })],
    ['revoked operator', provider.token({ email: gone })],
    ['another authorized party', provider.token({ email, azp: 'https://other-app.example' })],
    ['another issuer', provider.token({ email, iss: 'https://attacker.example' })],
    ['email not verified', provider.token({ email, email_verified: false })],
  ];
  const answers = new Set<string>();
  for (const [name, token] of refusals) {
    const response = await signIn(token);
    expect(response.status(), name).toBe(401);
    expect(response.headers()['set-cookie'], name).toBeUndefined();
    const body = await response.json() as { error: { code: string; message: string } };
    answers.add(`${body.error.code} ${body.error.message}`);
  }
  expect([...answers]).toEqual(['operator_sign_in_refused This account cannot sign in to the operator console.']);
  expect((await signIn(provider.token({ email }), 'https://evil.example')).status(), 'foreign origin').toBe(403);

  // The operator's first sign-in sets up the authenticator; no session before its code.
  const started = await signIn(provider.token({ email: email.toUpperCase() }));
  expect(started.status()).toBe(200);
  expect(started.headers()['set-cookie']).toBeUndefined();
  const step = await started.json() as { step: string; ticket: string; key: string; email: string };
  expect(step).toMatchObject({ step: 'enroll', email });
  const done = await page.request.post(`${baseURL}/api/v1/operator/auth/totp`, { data: { ticket: step.ticket, code: totp(step.key) } });
  expect(done.status()).toBe(200);
  const me = await page.request.get(`${baseURL}/api/v1/operator/auth/me`);
  expect(me.status()).toBe(200);
  expect(await me.json()).toMatchObject({ operator: { email, totp_enabled: true } });

  // The next sign-in asks for a code only, and the operator is listed with the authenticator on.
  const again = await signIn(provider.token({ email }));
  expect(await again.json()).toMatchObject({ step: 'code', email });
  expect(mustColliedesk(['operator', 'list'], { origin: baseURL, env: ring })).toContain(`${email}\tactive\tauthenticator on`);
});
