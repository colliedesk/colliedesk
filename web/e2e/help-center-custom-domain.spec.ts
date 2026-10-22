import { request as httpRequest } from 'node:http';
import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { setDNS } from './support/dns';
import { createWorkspaceWithSlug } from './support/workspace';

interface Answer { status: number; location?: string; type?: string; body: string }

// get asks the main server for path as a browser on host would: the server is reached by its address,
// the Host header names the custom domain (no DNS on the test machine knows it).
function get(host: string, path: string): Promise<Answer> {
  const port = Number(new URL(servers.main).port);
  return new Promise((resolve, reject) => {
    const call = httpRequest({ host: '127.0.0.1', port, path, headers: { Host: `${host}:${port}` } }, (response) => {
      let body = '';
      response.setEncoding('utf8');
      response.on('data', (chunk: string) => { body += chunk; });
      response.on('end', () => resolve({ status: response.statusCode ?? 0, location: response.headers.location, type: response.headers['content-type'], body }));
    });
    call.on('error', reject);
    call.end();
  });
}

test('help center custom domain: an admin sets it in Settings, the DNS check makes it active, the help center serves on it and nothing else, the old address moves there, and Remove reverses it', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url, slug } = createWorkspaceWithSlug(`e2e-custom-domain ${suffix}`, `admin-${suffix}@example.com`);
  const host = `help.e2e-${suffix}.example`;
  setDNS({});
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Domain help', locale: 'en' } })).status()).toBe(200);

  await page.goto(`${servers.main}/settings#help-center-heading`);
  await page.getByRole('textbox', { name: 'Host name' }).fill(host);
  await page.getByRole('button', { name: 'Set domain' }).click();
  await expect(page.getByText(`${host} is waiting for its DNS records.`, { exact: false })).toBeVisible();
  const txt = page.getByRole('group', { name: 'TXT record: proves the domain is yours' });
  const token = await txt.getByRole('textbox', { name: 'Value' }).inputValue();
  expect(token).toMatch(/^[0-9a-f]{32}$/);
  expect(await txt.getByRole('textbox', { name: 'Name' }).inputValue()).toBe(`_help-center-verify.${host}`);

  // Before the record exists the check says what it found; once the owner adds it, the check passes.
  await page.getByRole('button', { name: 'Check now' }).click();
  await expect(page.getByText(/Last check: _help-center-verify\./)).toBeVisible();
  setDNS({ txt: { [`_help-center-verify.${host}`]: [token] } });
  await page.getByRole('button', { name: 'Check now' }).click();
  await expect(page.getByRole('status').filter({ hasText: `The help center is served on ${host}.` })).toBeVisible();
  await expect(page.getByRole('link', { name: `http://${host}:${new URL(servers.main).port}` })).toBeVisible();

  const home = await get(host, '/');
  expect(home.status).toBe(200);
  expect(home.body).toContain('Domain help');
  for (const path of ['/api/v1/auth/me', '/mcp', '/settings', `/help/${slug}`]) {
    const other = await get(host, path);
    expect(other.status, path).toBe(404);
    expect(other.type, path).toContain('text/html');
  }
  const moved = await page.request.get(`${servers.main}/help/${slug}/search?q=start`, { maxRedirects: 0 });
  expect(moved.status()).toBe(301);
  expect(moved.headers().location).toBe(`http://${host}:${new URL(servers.main).port}/search?q=start`);
  expect((await page.request.get(`${servers.main}/api/v1/public/help-center/tls-ask?domain=${host}`)).status()).toBe(200);

  await page.getByRole('button', { name: 'Remove domain' }).click();
  await page.getByRole('group', { name: 'Remove the custom domain?' }).getByRole('button', { name: 'Remove' }).click();
  await expect(page.getByText('No custom domain is set.')).toBeVisible();
  expect((await get(host, '/')).body).not.toContain('Domain help');
  expect((await page.request.get(`${servers.main}/help/${slug}`, { maxRedirects: 0 })).status()).toBe(200);
  expect((await page.request.get(`${servers.main}/api/v1/public/help-center/tls-ask?domain=${host}`)).status()).toBe(404);
});
