import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

test('knowledge base: a save of a published article keeps the help center on the published text until Publish changes puts the new text live', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`KB Drafts ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Drafts help', locale: 'en' } })).status()).toBe(200);
  const { id: billing } = await (await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Billing' } })).json() as { id: string };
  const article = await (await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers,
    data: { collection_id: billing, title: 'Get a VAT invoice', body_md: 'Ask within 30 days.', status: 'published' } })).json() as { id: string; url: string };
  const { url: helpCenter } = await (await page.request.get(`${servers.main}/api/v1/help-center`)).json() as { url: string };
  const live = async () => (await page.request.get(article.url)).text();
  const found = async (q: string) => (await page.request.get(`${helpCenter}/search?q=${q}`)).text();

  await page.goto(`${servers.main}/knowledge/articles/${article.id}`);
  await page.getByRole('textbox', { name: 'Text (Markdown)' }).click();
  await page.keyboard.press('ControlOrMeta+End');
  await page.keyboard.type(' Or call the billing desk.');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  const changes = page.getByRole('group', { name: 'Unpublished changes' });
  await expect(changes).toContainText('The help center shows the published version until you publish these changes.');

  // Saved, not live: the public page and its search still have the published text.
  expect(await live()).toContain('Ask within 30 days.');
  expect(await live()).not.toContain('Or call the billing desk.');
  expect(await found('desk')).not.toContain('Get a VAT invoice');
  await changes.getByRole('button', { name: 'Compare with published' }).click();
  const compare = page.getByRole('dialog', { name: 'Unpublished changes' });
  await expect(compare.locator('li[data-change="added"]')).toHaveText('Added: Ask within 30 days. Or call the billing desk.');
  await compare.getByRole('button', { name: 'Close' }).click();

  // The list marks it, and keeps only such articles when asked.
  await page.goto(`${servers.main}/knowledge?has_draft=true`);
  await expect(page.getByRole('link', { name: /^Get a VAT invoice/ })).toContainText('Unpublished changes');

  await page.goto(`${servers.main}/knowledge/articles/${article.id}`);
  await page.getByRole('button', { name: 'Publish changes' }).click();
  await expect(page.getByRole('group', { name: 'Unpublished changes' })).toHaveCount(0);
  expect(await live()).toContain('Ask within 30 days. Or call the billing desk.');
  expect(await found('desk')).toContain('Get a VAT invoice');
});

test('knowledge base: a save of a published translation keeps its help center page on the published text until Publish changes puts the new text live', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`KB Translation Drafts ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Drafts help', locale: 'en' } })).status()).toBe(200);
  expect((await page.request.put(`${servers.main}/api/v1/help-center/languages`, { headers, data: { languages: [{ locale: 'fr', visible: true }] } })).status()).toBe(200);
  const { id: billing } = await (await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Billing' } })).json() as { id: string };
  const { id: article } = await (await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers,
    data: { collection_id: billing, title: 'Get a VAT invoice', body_md: 'Ask within 30 days.', status: 'published' } })).json() as { id: string };
  const created = await page.request.post(`${servers.main}/api/v1/kb/articles/${article}/translations`, { headers,
    data: { locale: 'fr', title: 'Obtenir une facture', body_md: 'Demandez-la sous 30 jours.', status: 'published' } });
  expect(created.status()).toBe(201);
  const { url: french } = await created.json() as { url: string };
  const live = async () => (await page.request.get(french)).text();

  await page.goto(`${servers.main}/knowledge/articles/${article}/translations/fr`);
  const text = page.getByRole('textbox', { name: 'Text' });
  await text.fill('Demandez-la sous 30 jours. Ou appelez le service facturation.');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  const changes = page.getByRole('group', { name: 'Unpublished changes' });
  await expect(changes).toContainText('The help center shows the published version until you publish these changes.');

  // Saved, not live: the French page still has the published text.
  expect(await live()).toContain('Demandez-la sous 30 jours.');
  expect(await live()).not.toContain('service facturation');

  // A reload opens on the saved changes, not on the published text.
  await page.reload();
  await expect(text).toHaveValue('Demandez-la sous 30 jours. Ou appelez le service facturation.');
  await changes.getByRole('button', { name: 'Publish changes' }).click();
  await expect(page.getByRole('group', { name: 'Unpublished changes' })).toHaveCount(0);
  expect(await live()).toContain('Ou appelez le service facturation.');
});
