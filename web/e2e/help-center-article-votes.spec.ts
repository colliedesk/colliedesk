import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

test('article votes: a reader answers No with what was missing on the public page, without script, and the team reads it in the editor', async ({ browser, page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url, slug } = createWorkspaceWithSlug(`e2e-votes ${suffix}`, `admin-${suffix}@example.com`);
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

  // The reader is someone else, with script turned off: the vote is a plain form that posts and comes back.
  const reader = await browser.newContext({ javaScriptEnabled: false });
  const article = await reader.newPage();
  await article.goto(`${servers.main}/help/${slug}/a/1-reset-your-password`);
  const box = article.getByRole('region', { name: 'Was this article helpful?' });
  await box.getByRole('button', { name: 'No' }).click();
  await expect(article).toHaveURL(/\?voted=no#helpful$/);
  await expect(box.getByRole('status')).toHaveText('Thanks for letting us know.');
  await box.getByRole('textbox', { name: 'What was missing?' }).fill('The screenshot shows the old menu, write to jane@acme.example or call 0912345678');
  await box.getByRole('button', { name: 'Send' }).click();
  await expect(box.getByRole('status')).toHaveText('Thanks, we will use it to improve this article.');
  await expect(box.getByText('0 out of 1 found this helpful')).toBeVisible();
  await reader.close();

  await page.goto(`${servers.main}/knowledge/articles/${id}`);
  const votes = page.getByRole('region', { name: 'Readers\' votes' });
  await expect(votes.getByText('0 out of 1 found this helpful')).toBeVisible();
  const row = votes.getByRole('listitem');
  await expect(row).toHaveCount(1);
  await expect(row).toContainText('No');
  await expect(row).toContainText('The screenshot shows the old menu, write to [email removed] or call [number removed]');
});
