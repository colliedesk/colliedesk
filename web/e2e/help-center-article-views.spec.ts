import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

// A desktop browser's user agent: Playwright's own says HeadlessChrome, which is counted as a bot.
const browserUA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36';

test('article views: a reader opens an article twice, Results shows one view, and the article editor shows it too', async ({ browser, page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url, slug } = createWorkspaceWithSlug(`e2e-views ${suffix}`, `admin-${suffix}@example.com`);
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
  const { id } = await posted.json() as { id: string };

  // The same reader twice within 30 minutes is one view.
  const reader = await browser.newContext({ userAgent: browserUA });
  const article = await reader.newPage();
  for (let i = 0; i < 2; i++) {
    await article.goto(`${servers.main}/help/${slug}/a/1-reset-your-password`);
    await expect(article.getByRole('heading', { name: 'Reset your password' })).toBeVisible();
  }
  await reader.close();

  // Views are written in the background: reload Results until the write has landed.
  const help = page.getByRole('region', { name: 'Help center' });
  await expect(async () => {
    await page.goto(`${servers.main}/results`);
    await expect(help.getByRole('table', { name: 'Articles' }).getByRole('row', { name: /Reset your password/ })).toHaveText(/^Reset your password1No votes0$/, { timeout: 1_000 });
  }).toPass({ timeout: 15_000 });
  await expect(help.getByRole('button', { name: '1', exact: true })).toBeVisible();

  await page.goto(`${servers.main}/knowledge/articles/${id}`);
  await expect(page.getByRole('region', { name: 'Last 30 days' }).getByText('1 view on the help center')).toBeVisible();
});
