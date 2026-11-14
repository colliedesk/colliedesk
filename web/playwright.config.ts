import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { ports, scratchDir } from './e2e/support/checkout';
import { defineConfig, devices } from '@playwright/test';
import { attachmentsDir, bootstrapEnv, databaseFor, databaseNameFor, goEnv, postgresURL, servers, settingsFile } from './e2e/support/cli';
import { ensureIdentityKey, identity, identityKeyFile, jwksURL } from './e2e/support/identity-provider';
import { inAppFreshdesk } from './e2e/support/fake-freshdesk';
import { dnsFile } from './e2e/support/dns';
import { mailpit } from './e2e/support/mailpit';

// The signing key exists before any server or worker starts: the JWKS process serves it and the workers sign with it.
ensureIdentityKey();

const localChrome = !process.env.CI && existsSync('/usr/bin/google-chrome') ? '/usr/bin/google-chrome' : undefined;

// Sign-in, the inbox from pairing to a sent reply, and the composer: the pages a browser engine is most
// likely to break (layout, contenteditable, the @ list).
const crossBrowserJourneys = ['sign-in-page.spec.ts', 'inbox-workflow.spec.ts', 'reply-composer.spec.ts'];
// WebKit drops the session cookie here: it is Secure (server/internal/httpapi/auth-handlers.go) and the test
// servers are plain http on localhost, which Chromium and Firefox treat as secure and WebKit does not. Over
// https, as on Cloud, Safari keeps it. So WebKit runs the journeys that need no session.
const webkitJourneys = ['sign-in-page.spec.ts'];

// Each server is the real `collie serve` on a database of its own, configured the way an operator
// configures one: the environment opens the database, and `collie config import` writes the settings
// into it. They differ in what is plugged in: nothing, an SMTP relay, cloud sign-in. Every run starts
// each server on a new database and migrates it, as a fresh install does.
function server(origin: string, env: Record<string, string> = {}, tags = '') {
  const run = tags ? `go run -tags ${tags} ./cmd/collie` : 'go run ./cmd/collie';
  const serve = `${run} serve --listen 127.0.0.1:${new URL(origin).port}`;
  const name = databaseNameFor(origin);
  const fresh = `psql '${postgresURL}' -q -v ON_ERROR_STOP=1 -c 'DROP DATABASE IF EXISTS ${name} WITH (FORCE)' -c 'CREATE DATABASE ${name}'`;
  const all = { EMAIL_TRUSTED_AUTHSERV_ID: 'mx.example.net', ...env };
  const file = settingsFile(name, all);
  return {
    command: `${fresh} && ${run} migrate && ${run} config import --replace ${file} && ${serve}`,
    cwd: '../server',
    env: {
      ...process.env as Record<string, string>, ...goEnv,
      APP_ORIGIN: origin, DATABASE_URL: databaseFor(origin), ATTACHMENTS_DIR: attachmentsDir, ...bootstrapEnv(all),
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
  use: { trace: 'retain-on-failure' },
  // Every test runs on Chromium, in every PR. The core journeys also run on WebKit (Safari's engine) and
  // Firefox, once a day in the cross-browser workflow, so a break in another browser shows without
  // making each PR wait for three browsers.
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'], launchOptions: { executablePath: localChrome } } },
    { name: 'webkit', use: devices['Desktop Safari'], testMatch: webkitJourneys },
    { name: 'firefox', use: devices['Desktop Firefox'], testMatch: crossBrowserJourneys },
  ],
  webServer: [
    // The JWKS the cloud server verifies sign-in tokens against. A process of its own, so it answers
    // while a test worker sits in a synchronous CLI call (the verifier gives a fetch five seconds).
    { command: `node e2e/support/jwks-server.mjs '${identityKeyFile}' ${identity.jwksPort}`, url: jwksURL, reuseExistingServer: false, timeout: 30_000 },
    // The main server also imports in the app from the fake Freshdesk (-tags e2e lets it reach a loopback
    // address), with a key ring as a fresh install has one. Its operator lets workspaces use an AI
    // provider on the local network, where the AI journeys run their fake model server; the others
    // allow it too, so an AI journey behaves the same on whichever server it runs.
    server(servers.main, { ...inAppFreshdesk.env(), MAILBOX_TOKEN_KEYS: 'k1:ZTJlLWluLWFwcC1pbXBvcnQta2V5LTMyLWJ5dGVzISE=', AI_ALLOW_PRIVATE_BASE_URLS: '1', COLLIEDESK_E2E_DNS_FILE: dnsFile,
      // Zalo's API for a Zalo channel is the stand-in a test starts (e2e/support/zalo.ts); nothing calls Zalo.
      COLLIEDESK_E2E_ZALO_URL: `http://127.0.0.1:${ports.zalo}` }, 'e2e'),
    server(servers.mail, { SMTP_HOST: '127.0.0.1', SMTP_PORT: mailpit.smtpPort, SMTP_STARTTLS: 'false', EMAIL_FROM_POLICY: 'any', AI_ALLOW_PRIVATE_BASE_URLS: '1' }),
    // The cloud server also imports in the app and sends replies by SMTP, through its own outbox worker,
    // for the golden path. It sends with the mail server's From policy, so a reply leaves both the same way.
    server(servers.cloud, {
      ...inAppFreshdesk.env(), MAILBOX_TOKEN_KEYS: 'c1:ZTJlLWNsb3VkLWltcG9ydC1rZXktMzItYnl0ZXMhISE=',
      SMTP_HOST: '127.0.0.1', SMTP_PORT: mailpit.smtpPort, SMTP_STARTTLS: 'false', EMAIL_FROM_POLICY: 'any',
      CLOUD_AUTH_ISSUER: identity.issuer, CLOUD_AUTH_JWKS_URL: jwksURL,
      CLOUD_AUTH_AUTHORIZED_PARTIES: identity.authorizedParty, CLERK_PUBLISHABLE_KEY: identity.publishableKey,
      // The hosted cloud: billing on (so trials, the lock and sign-up exist) with the settings sign-up refuses to start without.
      // The Polar values are never called; the journeys stop before checkout.
      POLAR_ACCESS_TOKEN: 'polar_oat_e2e', POLAR_WEBHOOK_SECRET: 'whsec_e2e', POLAR_API_URL: 'http://127.0.0.1:9',
      POLAR_PRODUCT_TEAM_MONTHLY: 'e2e-monthly', POLAR_PRODUCT_TEAM_YEARLY: 'e2e-yearly', POLAR_PRODUCT_FOUNDING: 'e2e-founding',
      SIGNUP_HASH_KEY: 'e2e-signup-hash-key-0123456789abcdef', TRUSTED_PROXIES: '127.0.0.1/32,::1/128', AI_ALLOW_PRIVATE_BASE_URLS: '1',
      // Built-in AI for reply drafts, as the hosted cloud has: the setup flow turns drafts on with it. Nothing
      // listens at its address on purpose. A customer message after drafts are on (the golden path's first
      // email) may start an automatic draft: its call is refused at once, the draft ends as failed, and no
      // journey reads drafts, so nothing waits on it. A journey that needs a written draft must first put a
      // model stand-in on ports.aiModel; it would otherwise fail on the draft's error, never pass by chance.
      AI_SERVER_PROVIDER: 'openai_compatible', AI_SERVER_BASE_URL: `http://127.0.0.1:${ports.aiModel}/v1`, AI_SERVER_MODELS: 'draft=e2e-draft', AI_SERVER_LABEL: 'Colliedesk AI',
    }, 'e2e'),
  ],
});
