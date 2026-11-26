import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

const headings = Array.from({ length: 12 }, (_, index) => `Step ${index + 1}`);
const body = ['The first paragraph of the guide.', ...headings.map((heading) => `## ${heading}\n\nWhat to do in ${heading.toLowerCase()}.`)].join('\n\n');

test('article contents: on a phone the list starts closed under the title and opens with one tap; on a wide screen the rail lists it open', async ({ browser, page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url, slug } = createWorkspaceWithSlug(`e2e-contents ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Acme help', locale: 'en' } })).status()).toBe(200);
  const created = await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Guides' } });
  const { id: collection } = await created.json() as { id: string };
  const posted = await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers, data: {
    collection_id: collection, title: 'Set up the shop', status: 'published', body_md: body } });
  expect(posted.status()).toBe(201);
  const address = `${servers.main}/help/${slug}/a/1-set-up-the-shop`;

  // A phone, without script as the help center runs: the text comes first and the list waits closed.
  const phone = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 390, height: 844 } });
  const reader = await phone.newPage();
  await reader.goto(address);
  const toggle = reader.getByRole('group').getByText('On this page (12)');
  await expect(toggle).toBeVisible();
  await expect(reader.locator('details.contents-top')).not.toHaveAttribute('open');
  await expect(reader.getByText('The first paragraph of the guide.')).toBeInViewport();
  await expect(reader.locator('.contents-top').getByRole('link', { name: 'Step 12' })).toBeHidden();
  expect(await reader.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await reader.screenshot({ path: test.info().outputPath('contents-phone-closed.png') });

  await toggle.click();
  await expect(reader.locator('details.contents-top')).toHaveAttribute('open', '');
  await reader.locator('.contents-top').getByRole('link', { name: 'Step 12' }).click();
  await expect(reader).toHaveURL(`${address}#step-12`);
  await expect(reader.getByRole('heading', { level: 2, name: 'Step 12' })).toBeInViewport();
  await phone.close();

  // A wide screen: the rail lists the headings open, with no toggle.
  const desk = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 1280, height: 900 } });
  const wide = await desk.newPage();
  await wide.goto(address);
  await expect(wide.locator('.contents-rail').getByRole('link', { name: 'Step 1', exact: true })).toBeVisible();
  await expect(wide.locator('.contents-rail').getByRole('link', { name: 'Step 12' })).toBeVisible();
  await expect(wide.locator('details.contents-top')).toBeHidden();
  await wide.screenshot({ path: test.info().outputPath('contents-wide-rail.png') });
  await desk.close();
});
