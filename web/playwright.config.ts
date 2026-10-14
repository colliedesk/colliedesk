import { existsSync } from 'node:fs';
import { defineConfig } from '@playwright/test';

const baseURL = 'http://localhost:18080';
const databaseURL = process.env.TEST_DATABASE_URL
  ?? 'postgres://colliedesk:colliedesk@127.0.0.1:55441/colliedesk_test?sslmode=disable';
const localChrome = !process.env.CI && existsSync('/usr/bin/google-chrome') ? '/usr/bin/google-chrome' : undefined;

export default defineConfig({
  testDir: './e2e',
  outputDir: '/tmp/colliedesk-playwright-results',
  reporter: 'list',
  fullyParallel: false,
  workers: 1,
  use: { baseURL, trace: 'retain-on-failure', launchOptions: { executablePath: localChrome } },
  webServer: {
    // serve refuses to start until row-level security is migrated, so migrate first like compose does.
    command: 'go run ./cmd/colliedesk migrate && go run ./cmd/colliedesk serve --listen 127.0.0.1:18080',
    cwd: '../server',
    env: {
      ...process.env,
      APP_ORIGIN: baseURL,
      DATABASE_URL: databaseURL,
      ATTACHMENTS_DIR: '/tmp/colliedesk-e2e-attachments',
      EMAIL_TRUSTED_AUTHSERV_ID: 'mx.example.net',
      GOPATH: '/tmp/colliedesk-gopath',
      GOMODCACHE: '/tmp/colliedesk-gopath/pkg/mod',
      GOCACHE: '/tmp/colliedesk-go-cache',
    },
    url: baseURL,
    reuseExistingServer: false,
    timeout: 120_000,
  },
});
