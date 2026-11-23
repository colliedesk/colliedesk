import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

test('article print: a reader without script opens the print page, which shows the article alone and prints without its own links', async ({ browser, page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url, slug } = createWorkspaceWithSlug(`e2e-print ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Acme help', locale: 'en' } })).status()).toBe(200);
  const created = await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Guides', description: 'How to use Acme' } });
  const { id: collection } = await created.json() as { id: string };
  const posted = await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers, data: {
    collection_id: collection, title: 'Reset your password', status: 'published', body_md: 'Open **Settings**.' } });
  expect(posted.status()).toBe(201);

  const reader = await browser.newContext({ javaScriptEnabled: false });
  const article = await reader.newPage();
  const address = `${servers.main}/help/${slug}/a/1-reset-your-password`;
  await article.goto(address);
  await expect(article.getByRole('banner')).toBeVisible();
  await article.getByRole('link', { name: 'Print', exact: true }).click();
  await expect(article).toHaveURL(`${address}/print`);

  await expect(article.getByRole('heading', { level: 1, name: 'Reset your password' })).toBeVisible();
  await expect(article.getByText('Open Settings.')).toBeVisible();
  await expect(article.locator('.masthead, .foot, .rail, .helpful, .chatbox')).toHaveCount(0);
  const back = article.getByRole('link', { name: 'Back to the article' });
  await expect(back).toBeVisible();

  // On paper only the article is left: the way back and the hint are for the screen.
  await article.emulateMedia({ media: 'print' });
  await expect(back).toBeHidden();
  await expect(article.getByText('Open Settings.')).toBeVisible();
  await article.emulateMedia({ media: 'screen' });

  await back.click();
  await expect(article).toHaveURL(address);
  await reader.close();
});
