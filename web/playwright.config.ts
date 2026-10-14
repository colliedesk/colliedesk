import { existsSync } from 'node:fs';
import { defineConfig } from '@playwright/test';
import { attachmentsDir, databaseURL, goEnv, servers } from './e2e/support/cli';
import { identity, jwksURL } from './e2e/support/identity-provider';
import { mailpit } from './e2e/support/mailpit';

const localChrome = !process.env.CI && existsSync('/usr/bin/google-chrome') ? '/usr/bin/google-chrome' : undefined;

// Each server is the real `colliedesk serve` on the same database, configured only through the
// environment an operator sets. They differ in what is plugged in: nothing, an SMTP relay, cloud sign-in.
// serve refuses to start until row-level security is migrated: the main server migrates first, like
// compose does, and the others wait for it so two migrations never race on the shared database.
function server(origin: string, env: Record<string, string> = {}) {
  const serve = `go run ./cmd/colliedesk serve --listen 127.0.0.1:${new URL(origin).port}`;
  const first = origin === servers.main;
  return {
    command: first ? `go run ./cmd/colliedesk migrate && ${serve}` : `node ../web/e2e/support/wait-for-url.mjs ${servers.main} && ${serve}`,
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
  outputDir: '/tmp/colliedesk-playwright-results',
  reporter: 'list',
  fullyParallel: false,
  workers: 1,
  use: { trace: 'retain-on-failure', launchOptions: { executablePath: localChrome } },
  webServer: [
    server(servers.main),
    server(servers.mail, { SMTP_HOST: '127.0.0.1', SMTP_PORT: mailpit.smtpPort, SMTP_STARTTLS: 'false', EMAIL_FROM_POLICY: 'any' }),
    server(servers.cloud, {
      CLOUD_AUTH_ISSUER: identity.issuer, CLOUD_AUTH_JWKS_URL: jwksURL,
      CLOUD_AUTH_AUTHORIZED_PARTIES: identity.authorizedParty, CLERK_PUBLISHABLE_KEY: identity.publishableKey,
    }),
  ],
});
