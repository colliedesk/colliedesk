import { execFileSync } from 'node:child_process';
import { expect, test } from '@playwright/test';
import { databaseURL, servers } from './support/cli';

const baseURL = servers.main;

test('connects email, receives a customer email and shows why the reply is not emailed', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const pairingURL = execFileSync('go', [
    'run', './cmd/collie', 'create-workspace',
    '--name', `E2E Mail ${suffix}`, '--admin-email', `admin-${suffix}@example.com`,
  ], {
    cwd: '../server', encoding: 'utf8',
    env: {
      ...process.env, APP_ORIGIN: baseURL, DATABASE_URL: databaseURL,
      GOPATH: '/tmp/colliedesk-gopath', GOMODCACHE: '/tmp/colliedesk-gopath/pkg/mod', GOCACHE: '/tmp/colliedesk-go-cache',
    },
  }).trim();
  await page.goto(pairingURL);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${baseURL}/`);

  await page.goto(`${baseURL}/settings`);
  await page.getByRole('textbox', { name: 'Support address' }).fill(`support-${suffix}@example.com`);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await page.getByRole('button', { name: 'Create inbound secret' }).click();
  const secret = await page.getByRole('textbox', { name: /Inbound secret/ }).inputValue();
  expect(secret).toContain('.');
  await expect(page.getByText(/Outgoing email is not configured/)).toBeVisible();
  await expect(page.getByText(/Replies cannot be emailed yet/)).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'TXT record name' })).toHaveValue(`_colliedesk.example.com`);

  const raw = [
    `From: Erin Customer <erin-${suffix}@customer.example>`,
    `To: support-${suffix}@example.com`,
    'Subject: E2E email thread',
    `Message-ID: <e2e-${suffix}@customer.example>`,
    'Authentication-Results: mx.example.net; dmarc=fail header.from=customer.example',
    'MIME-Version: 1.0',
    'Content-Type: multipart/mixed; boundary="e2e"',
    '',
    '--e2e',
    'Content-Type: text/plain; charset=utf-8',
    '',
    'My order never arrived.',
    '--e2e',
    'Content-Type: application/pdf; name="receipt.pdf"',
    'Content-Disposition: attachment; filename="receipt.pdf"',
    'Content-Transfer-Encoding: base64',
    '',
    Buffer.from('%PDF-1.4 e2e receipt').toString('base64'),
    '--e2e--',
  ].join('\r\n');
  const inbound = await page.request.post(`${baseURL}/api/v1/channels/email/inbound`, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret }, data: Buffer.from(raw),
  });
  expect(inbound.status()).toBe(200);
  expect((await inbound.json() as { status: string }).status).toBe('created');

  await page.goto(`${baseURL}/`);
  await page.getByRole('button', { name: /E2E email thread/i }).click();
  await expect(page.getByText('My order never arrived.')).toBeVisible();
  await expect(page.getByText(`erin-${suffix}@customer.example`)).toBeVisible();
  await expect(page.getByText('Failed sender verification')).toBeVisible();
  const receipt = page.getByRole('link', { name: 'receipt.pdf' });
  await expect(receipt).toBeVisible();
  const download = await page.request.get(`${baseURL}${await receipt.getAttribute('href')}`);
  expect(await download.text()).toBe('%PDF-1.4 e2e receipt');
  await expect(page.getByRole('link', { name: 'Download original email' })).toBeVisible();
  await expect(page.getByRole('status')).toHaveText(/Outgoing email is not configured on this server/);

  await page.getByLabel('Message body').last().fill('We are on it.');
  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(page.getByText('We are on it.')).toBeVisible();
  await expect(page.getByText('Delivery failed')).toBeVisible();
  await expect(page.getByText('Delivery failed')).toHaveAttribute('title', /not configured/);
});
