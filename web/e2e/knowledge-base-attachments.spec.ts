import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

// A PDF the server takes: its first bytes say so, and the rest pads it to the size asked.
const pdf = (size: number) => Buffer.concat([Buffer.from('%PDF-1.4\n'), Buffer.alloc(size - 9, 'x')]);

test('knowledge base: a file attached to a published article downloads from the help center, and replacing it in the media library keeps its address and shows the new size', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`KB Files ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Files help', locale: 'en' } })).status()).toBe(200);
  const { id: collection } = await (await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Returns' } })).json() as { id: string };
  const article = await (await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers,
    data: { collection_id: collection, title: 'Warranty', body_md: 'Fill in the form.', status: 'published' } })).json() as { id: string; url: string };

  // The editor's Attachments strip uploads the file; on a published article it waits in the changes until they are published.
  await page.goto(`${servers.main}/knowledge/articles/${article.id}`);
  const strip = page.getByRole('region', { name: 'Attachments' });
  await strip.getByLabel('Choose files').setInputFiles({ name: 'Warranty form.pdf', mimeType: 'application/pdf', buffer: pdf(1024) });
  await expect(strip.getByRole('link', { name: 'Warranty form.pdf' })).toBeVisible();
  await expect(strip.getByText('PDF · 1.0 KiB')).toBeVisible();
  await page.getByRole('button', { name: 'Publish changes' }).click();
  await expect(page.getByRole('button', { name: 'Publish changes' })).toHaveCount(0);

  await page.goto(article.url);
  const link = page.locator('.downloads-list a');
  await expect(link).toContainText('Warranty form.pdf');
  const address = await link.getAttribute('href');
  expect(address).toMatch(/\/files\/[0-9a-f-]{36}\/warranty-form\.pdf$/);
  const first = await page.request.get(new URL(address!, article.url).href);
  expect(first.status()).toBe(200);
  expect(first.headers()['content-disposition']).toBe('attachment; filename*=UTF-8\'\'Warranty%20form.pdf');
  expect(first.headers()['x-content-type-options']).toBe('nosniff');
  expect(first.headers()['content-security-policy']).toBe("default-src 'none'; sandbox");
  expect((await first.body()).length).toBe(1024);

  // Replace in the media library takes the new version from disk; the file keeps its id, so every link to it stays.
  await page.goto(`${servers.main}/knowledge/media?kind=file`);
  await page.getByRole('button', { name: /^Warranty form\.pdf, file,/ }).click();
  const panel = page.getByRole('complementary', { name: 'Warranty form.pdf' });
  await panel.getByLabel('Choose the new version').setInputFiles({ name: 'Warranty form.pdf', mimeType: 'application/pdf', buffer: pdf(3072) });
  await expect(page.getByText('Replaced. The articles that list it offer the new version at the same address.')).toBeVisible();
  await panel.getByRole('tab', { name: 'File' }).click();
  await expect(panel.getByText('3.0 KiB')).toBeVisible();

  await page.goto(article.url);
  await expect(page.locator('.downloads-list a')).toHaveAttribute('href', address!);
  const second = await page.request.get(new URL(address!, article.url).href);
  expect(second.status()).toBe(200);
  expect((await second.body()).length).toBe(3072);
});
