import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

test('knowledge base list: tags set in the editor, a hidden column stays hidden after a reload, and a saved view opens its filters again', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`KB List ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  const menu = (name: string) => page.locator('summary').filter({ hasText: new RegExp(`^${name}`) });
  const { id: billing } = await (await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Billing' } })).json() as { id: string };
  expect((await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers,
    data: { collection_id: billing, title: 'Refund rules', body_md: 'Refunds within 30 days.', status: 'published', tags: ['billing'] } })).status()).toBe(201);
  const draft = await (await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers,
    data: { collection_id: billing, title: 'Change your plan', body_md: 'Open Billing.', status: 'draft' } })).json() as { id: string };

  // Tags are typed in the editor and saved with the text, written as the server stores them.
  await page.goto(`${servers.main}/knowledge/articles/${draft.id}`);
  const addTag = page.getByRole('combobox', { name: 'Add a tag' });
  await addTag.fill('  Plans  And Pricing ');
  await addTag.press('Enter');
  await expect(page.getByRole('list', { name: 'Tags' })).toContainText('plans and pricing');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect.poll(async () => ((await (await page.request.get(`${servers.main}/api/v1/kb/articles/${draft.id}`)).json()) as { tags: string[] }).tags)
    .toEqual(['plans and pricing']);

  // A hidden column is kept for this person: the list comes back without it after a reload.
  await page.goto(`${servers.main}/knowledge`);
  const refund = page.getByRole('link', { name: /^Refund rules/ });
  await expect(refund).toContainText('billing');
  await menu('Columns').click();
  const saved = page.waitForResponse((response) => response.url().endsWith('/api/v1/agents/me/kb-list-columns') && response.request().method() === 'PUT');
  // A click, not uncheck(): the unticked column moves below the shown ones, which uncheck() reads as no change.
  await page.getByRole('group', { name: 'Columns' }).getByRole('checkbox', { name: 'Tags' }).click();
  expect((await saved).status()).toBe(200);
  await expect(page.getByRole('group', { name: 'Columns' }).getByRole('checkbox', { name: 'Tags' })).not.toBeChecked();
  await page.reload();
  await expect(page.getByRole('link', { name: /^Refund rules/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /^Refund rules/ })).not.toContainText('billing');
  await menu('Columns').click();
  await expect(page.getByRole('group', { name: 'Columns' }).getByRole('checkbox', { name: 'Tags' })).not.toBeChecked();
  await page.getByRole('group', { name: 'Columns' }).getByRole('button', { name: 'Reset columns' }).click();
  await expect(page.getByRole('link', { name: /^Refund rules/ })).toContainText('billing');

  // A view keeps the filters on screen; opening it from the plain list brings them back.
  await menu('Filters').click();
  await page.getByRole('group', { name: 'Filters' }).getByRole('checkbox', { name: 'billing (1)' }).check();
  await expect(page.getByRole('link', { name: /^Change your plan/ })).toHaveCount(0);
  await menu('Views').click();
  const views = page.getByRole('group', { name: 'Views' });
  await views.getByRole('textbox', { name: 'View name' }).fill('Billing tag');
  await views.getByRole('button', { name: 'Save view' }).click();
  await expect(views.getByRole('button', { name: 'Billing tag' })).toBeVisible();

  await page.goto(`${servers.main}/knowledge`);
  await expect(page.getByRole('link', { name: /^Change your plan/ })).toBeVisible();
  await menu('Views').click();
  await page.getByRole('group', { name: 'Views' }).getByRole('button', { name: 'Billing tag' }).click();
  await expect(page.getByRole('link', { name: /^Change your plan/ })).toHaveCount(0);
  await expect(page.getByRole('link', { name: /^Refund rules/ })).toBeVisible();
  expect(new URL(page.url()).searchParams.get('tag')).toContain('billing');

  // The CSV export keeps what the same filters keep.
  const csv = await (await page.request.get(`${servers.main}/api/v1/kb/articles/export.csv?tag=billing`)).text();
  expect(csv).toContain('Refund rules');
  expect(csv).not.toContain('Change your plan');
});
