import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

test('several help centers: an admin adds one in Settings, shows a collection on it only, turns it on, and readers find the collection there and not on the primary; deleting it takes its pages off line', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url, slug } = createWorkspaceWithSlug(`e2e-several-help-centers ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Acme help', locale: 'en' } })).status()).toBe(200);
  const created = await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Printers', description: 'Set up a printer' } });
  const { id: collection } = await created.json() as { id: string };
  expect((await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers, data: {
    collection_id: collection, title: 'Load paper', status: 'published', body_md: 'Open the tray.',
  } })).status()).toBe(201);
  const base = `${servers.main}/help/${slug}`;

  // Add: name and handle, and the address the handle makes.
  await page.goto(`${servers.main}/knowledge/settings`);
  await page.getByRole('button', { name: 'New help center' }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Hardware');
  await page.getByRole('textbox', { name: 'Handle', exact: true }).fill('hardware');
  await page.getByRole('button', { name: 'Create help center' }).click();
  const list = page.getByRole('list', { name: 'Help centers' });
  await expect(list.getByRole('button', { name: 'Turn on Hardware' })).toBeVisible();

  // Shown on: the new help center only.
  await page.goto(`${servers.main}/knowledge`);
  await page.getByRole('link', { name: 'Printers, 1 article' }).click();
  await page.getByRole('button', { name: 'Edit Printers' }).click();
  const form = page.getByRole('form', { name: 'Edit Printers' });
  const shownOn = form.getByRole('group', { name: 'Shown on' });
  await shownOn.getByRole('checkbox', { name: 'Hardware' }).check();
  await shownOn.getByRole('checkbox').first().uncheck();
  await expect(shownOn.getByRole('checkbox', { name: 'Hardware' })).toBeDisabled();
  await form.getByRole('button', { name: 'Save collection' }).click();
  await expect(form).toBeHidden();

  // Off, the new help center has no pages; on, its home lists the collection and the primary's does not.
  expect((await page.request.get(`${base}/hardware`)).status()).toBe(404);
  await page.goto(`${servers.main}/knowledge/settings`);
  await list.getByRole('button', { name: 'Turn on Hardware' }).click();
  await expect(list.getByRole('button', { name: 'Turn off Hardware' })).toBeVisible();
  await page.goto(`${base}/hardware`);
  await expect(page.getByRole('link', { name: 'Printers', exact: true })).toBeVisible();
  await page.goto(base);
  await expect(page.getByRole('link', { name: 'Printers', exact: true })).toHaveCount(0);

  // Delete: the pages go off line at once; the collection stays.
  await page.goto(`${servers.main}/knowledge/settings`);
  await list.getByRole('button', { name: 'Delete Hardware' }).click();
  await list.getByRole('button', { name: 'Delete help center' }).click();
  await expect(list.getByRole('button', { name: 'Delete Hardware' })).toHaveCount(0);
  expect((await page.request.get(`${base}/hardware`)).status()).toBe(404);
  const listed = await page.request.get(`${servers.main}/api/v1/kb/collections`);
  expect(listed.status()).toBe(200);
  const { items } = await listed.json() as { items: { id: string }[] };
  expect(items.map((item) => item.id)).toEqual([collection]);
});
