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
    command: 'go run ./cmd/colliedesk serve --listen 127.0.0.1:18080',
    cwd: '../server',
    env: {
      ...process.env,
      APP_ORIGIN: baseURL,
      DATABASE_URL: databaseURL,
      GOPATH: '/tmp/colliedesk-gopath',
      GOMODCACHE: '/tmp/colliedesk-gopath/pkg/mod',
      GOCACHE: '/tmp/colliedesk-go-cache',
    },
    url: baseURL,
    reuseExistingServer: false,
    timeout: 120_000,
  },
});
