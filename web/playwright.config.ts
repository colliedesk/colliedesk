import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { scratchDir } from './e2e/support/checkout';
import { defineConfig } from '@playwright/test';
import { attachmentsDir, databaseURL, goEnv, servers } from './e2e/support/cli';
import { identity, jwksURL } from './e2e/support/identity-provider';
import { inAppFreshdesk } from './e2e/support/fake-freshdesk';
import { mailpit } from './e2e/support/mailpit';

const localChrome = !process.env.CI && existsSync('/usr/bin/google-chrome') ? '/usr/bin/google-chrome' : undefined;

// Each server is the real `collie serve` on the same database, configured only through the
// environment an operator sets. They differ in what is plugged in: nothing, an SMTP relay, cloud sign-in.
// serve refuses to start until row-level security is migrated: the main server migrates first, like
// compose does, and the others wait for it so two migrations never race on the shared database.
function server(origin: string, env: Record<string, string> = {}, tags = '') {
  const run = tags ? `go run -tags ${tags} ./cmd/collie` : 'go run ./cmd/collie';
  const serve = `${run} serve --listen 127.0.0.1:${new URL(origin).port}`;
  const first = origin === servers.main;
  return {
    command: first ? `${run} migrate && ${serve}` : `node ../web/e2e/support/wait-for-url.mjs ${servers.main} && ${serve}`,
    cwd: '../server',
    env: {
      ...process.env as Record<string, string>, ...goEnv,
      APP_ORIGIN: origin, DATABASE_URL: databaseURL, ATTACHMENTS_DIR: attachmentsDir, EMAIL_TRUSTED_AUTHSERV_ID: 'mx.example.net', ...env,
    },
    url: origin,
    reuseExistingServer: false,
    timeout: 120_000,
  };
}

export default defineConfig({
  testDir: './e2e',
  outputDir: join(scratchDir, 'results'),
  reporter: 'list',
  fullyParallel: false,
  // A stray test.only would silently narrow both CI jobs to one test.
  forbidOnly: !!process.env.CI,
  workers: 1,
  use: { trace: 'retain-on-failure', launchOptions: { executablePath: localChrome } },
  webServer: [
    // The main server also imports in the app from the fake Freshdesk (-tags e2e lets it reach a loopback
    // address), with a key ring as a fresh install has one.
    server(servers.main, { ...inAppFreshdesk.env(), MAILBOX_TOKEN_KEYS: 'k1:ZTJlLWluLWFwcC1pbXBvcnQta2V5LTMyLWJ5dGVzISE=' }, 'e2e'),
    server(servers.mail, { SMTP_HOST: '127.0.0.1', SMTP_PORT: mailpit.smtpPort, SMTP_STARTTLS: 'false', EMAIL_FROM_POLICY: 'any' }),
    // The cloud server also imports in the app and sends replies by SMTP, for the golden path. Every server
    // with a relay runs an outbox worker that sends any workspace's queued reply, and the worker picks the
    // From line, so it uses the mail server's policy: either worker sends a reply the same way. Its key
    // ring has its own key id, so the main server never runs a cloud import (and the other way round).
    server(servers.cloud, {
      ...inAppFreshdesk.env(), MAILBOX_TOKEN_KEYS: 'c1:ZTJlLWNsb3VkLWltcG9ydC1rZXktMzItYnl0ZXMhISE=',
      SMTP_HOST: '127.0.0.1', SMTP_PORT: mailpit.smtpPort, SMTP_STARTTLS: 'false', EMAIL_FROM_POLICY: 'any',
      CLOUD_AUTH_ISSUER: identity.issuer, CLOUD_AUTH_JWKS_URL: jwksURL,
      CLOUD_AUTH_AUTHORIZED_PARTIES: identity.authorizedParty, CLERK_PUBLISHABLE_KEY: identity.publishableKey,
      // The hosted cloud: billing on (so trials, the lock and sign-up exist) with the settings sign-up refuses to start without.
      // The Polar values are never called; the journeys stop before checkout.
      POLAR_ACCESS_TOKEN: 'polar_oat_e2e', POLAR_WEBHOOK_SECRET: 'whsec_e2e', POLAR_API_URL: 'http://127.0.0.1:9',
      POLAR_PRODUCT_TEAM_MONTHLY: 'e2e-monthly', POLAR_PRODUCT_TEAM_YEARLY: 'e2e-yearly', POLAR_PRODUCT_FOUNDING: 'e2e-founding',
      SIGNUP_HASH_KEY: 'e2e-signup-hash-key-0123456789abcdef', TRUSTED_PROXIES: '127.0.0.1/32,::1/128',
    }, 'e2e'),
  ],
});
