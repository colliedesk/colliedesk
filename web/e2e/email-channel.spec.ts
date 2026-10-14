import { execFileSync } from 'node:child_process';
import { expect, test } from '@playwright/test';

const baseURL = 'http://localhost:18080';
const databaseURL = process.env.TEST_DATABASE_URL
  ?? 'postgres://colliedesk:colliedesk@127.0.0.1:55441/colliedesk_test?sslmode=disable';

test('connects email, receives a customer email and queues the reply', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const pairingURL = execFileSync('go', [
    'run', './cmd/colliedesk', 'create-workspace',
    '--name', `E2E Mail ${suffix}`, '--admin-email', `admin-${suffix}@example.com`,
  ], {
    cwd: '../server', encoding: 'utf8',
    env: {
      ...process.env, APP_ORIGIN: baseURL, DATABASE_URL: databaseURL,
      GOPATH: '/tmp/colliedesk-gopath', GOMODCACHE: '/tmp/colliedesk-gopath/pkg/mod', GOCACHE: '/tmp/colliedesk-go-cache',
    },
  }).trim();
  await page.goto(pairingURL);
  await page.getByRole('button', { name: 'Continue to CollieDesk' }).click();
  await expect(page).toHaveURL(`${baseURL}/`);

  await page.goto(`${baseURL}/settings`);
  await page.getByRole('textbox', { name: 'Support address' }).fill(`support-${suffix}@example.com`);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await page.getByRole('button', { name: 'Create inbound secret' }).click();
  const secret = await page.getByRole('textbox', { name: /Inbound secret/ }).inputValue();
  expect(secret).toContain('.');
  await expect(page.getByText(/Outgoing email is not configured/)).toBeVisible();

  const raw = [
    `From: Erin Customer <erin-${suffix}@customer.example>`,
    `To: support-${suffix}@example.com`,
    'Subject: E2E email thread',
    `Message-ID: <e2e-${suffix}@customer.example>`,
    'Authentication-Results: mx.example.net; dmarc=fail header.from=customer.example',
    '',
    'My order never arrived.',
  ].join('\r\n');
  const inbound = await page.request.post(`${baseURL}/api/v1/channels/email/inbound`, {
    headers: { 'Content-Type': 'message/rfc822', 'X-CollieDesk-Inbound-Secret': secret }, data: Buffer.from(raw),
  });
  expect(inbound.status()).toBe(200);
  expect((await inbound.json() as { status: string }).status).toBe('created');

  await page.goto(`${baseURL}/`);
  await page.getByRole('button', { name: /E2E email thread/i }).click();
  await expect(page.getByText('My order never arrived.')).toBeVisible();
  await expect(page.getByText(`erin-${suffix}@customer.example`)).toBeVisible();
  await expect(page.getByText('Failed sender verification')).toBeVisible();

  await page.getByLabel('Message body').last().fill('We are on it.');
  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(page.getByText('We are on it.')).toBeVisible();
  await expect(page.getByText('Queued')).toBeVisible();
});
