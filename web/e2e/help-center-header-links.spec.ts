import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

test('help center header: Home and Solutions stay in the header row on a phone and lead to the start and to every collection', async ({ browser, page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url, slug } = createWorkspaceWithSlug(`e2e-tabs ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'A help center with a long name for a phone', locale: 'en' } })).status()).toBe(200);
  const created = await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Guides' } });
  const { id: collection } = await created.json() as { id: string };
  expect((await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers, data: {
    collection_id: collection, title: 'Reset your password', status: 'published', body_md: 'Open **Settings**.' } })).status()).toBe(201);

  const phone = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 390, height: 844 } });
  const reader = await phone.newPage();
  await reader.goto(`${servers.main}/help/${slug}/a/1-reset-your-password`);
  const menu = reader.getByRole('navigation', { name: 'Help center' });
  const solutions = menu.getByRole('link', { name: 'Solutions' });
  await expect(menu.getByRole('link', { name: 'Home' })).toBeInViewport();
  await expect(solutions).toBeInViewport();
  await expect(solutions).toHaveAttribute('aria-current', 'page');
  expect(await reader.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await reader.screenshot({ path: test.info().outputPath('header-links-phone.png') });

  await solutions.click();
  await expect(reader).toHaveURL(`${servers.main}/help/${slug}/c/`);
  await expect(reader.getByRole('heading', { level: 1, name: 'Solutions' })).toBeVisible();
  await expect(reader.getByRole('link', { name: 'Reset your password' })).toBeVisible();
  await menu.getByRole('link', { name: 'Home' }).click();
  await expect(reader.getByRole('heading', { level: 1, name: 'How can we help?' })).toBeVisible();
  await phone.close();
});
