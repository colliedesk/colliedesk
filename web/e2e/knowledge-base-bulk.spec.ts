import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

const headers = { Origin: servers.main };
type Article = { id: string; collection_id: string; tags: string[] };

async function signIn(page: Page, name: string) {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`${name} ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
}

async function collection(page: Page, name: string) {
  return (await (await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name } })).json() as { id: string }).id;
}

async function draft(page: Page, collectionID: string, title: string, tags: string[] = []) {
  const response = await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers,
    data: { collection_id: collectionID, title, body_md: `${title}.`, status: 'draft', tags } });
  expect(response.status()).toBe(201);
  return (await response.json() as Article).id;
}

const article = async (page: Page, id: string) => (await (await page.request.get(`${servers.main}/api/v1/kb/articles/${id}`)).json()) as Article;

test('knowledge base bulk: every article a filter matches gets a tag at once, and Undo takes it off again', async ({ page }) => {
  await signIn(page, 'KB Bulk Tags');
  const billing = await collection(page, 'Billing');
  const refund = await draft(page, billing, 'Refund rules', ['billing']);
  const invoices = await draft(page, billing, 'Invoices', ['billing']);
  const plan = await draft(page, billing, 'Change your plan');

  await page.goto(`${servers.main}/knowledge?tag=billing`);
  await expect(page.getByRole('link', { name: /^Change your plan/ })).toHaveCount(0);
  await page.getByRole('checkbox', { name: 'Select Refund rules' }).check();
  const bar = page.getByRole('group', { name: 'Selected articles' });
  await bar.getByRole('button', { name: 'Select all matching articles' }).click();
  await expect(bar).toContainText('All matching articles selected');
  await bar.locator('summary[aria-label="Add tag"]').click();
  const adding = bar.getByRole('group', { name: 'Add tag' });
  await adding.getByRole('textbox', { name: 'New tag' }).fill('Refunds');
  await adding.getByRole('button', { name: 'Add' }).click();
  await expect(page.getByText('2 articles changed.')).toBeVisible();
  expect((await article(page, refund)).tags).toEqual(['billing', 'refunds']);
  expect((await article(page, invoices)).tags).toEqual(['billing', 'refunds']);
  expect((await article(page, plan)).tags).toEqual([]);

  await page.getByRole('button', { name: 'Undo' }).click();
  await expect(page.getByText('2 articles put back.')).toBeVisible();
  expect((await article(page, refund)).tags).toEqual(['billing']);
  expect((await article(page, invoices)).tags).toEqual(['billing']);
});

test('knowledge base bulk: 50 articles move to another collection at once, and Undo puts every one back', async ({ page }) => {
  await signIn(page, 'KB Bulk Move');
  const billing = await collection(page, 'Billing');
  const archive = await collection(page, 'Archive');
  const ids: string[] = [];
  for (let index = 1; index <= 50; index++) ids.push(await draft(page, billing, `Billing question ${index}`));

  await page.goto(`${servers.main}/knowledge?collection=${billing}`);
  await page.getByRole('checkbox', { name: 'Select Billing question 1', exact: true }).check();
  const bar = page.getByRole('group', { name: 'Selected articles' });
  await bar.getByRole('button', { name: 'Select all matching articles' }).click();
  await bar.locator('summary[aria-label="Move to collection"]').click();
  await bar.getByRole('group', { name: 'Move to collection' }).getByRole('button', { name: 'Archive', exact: true }).click();
  await expect(page.getByText('50 articles changed.')).toBeVisible();
  const moved = await Promise.all(ids.map((id) => article(page, id)));
  expect(moved.filter((item) => item.collection_id === archive)).toHaveLength(50);

  await page.getByRole('button', { name: 'Undo' }).click();
  await expect(page.getByText('50 articles put back.')).toBeVisible();
  const back = await Promise.all(ids.map((id) => article(page, id)));
  expect(back.filter((item) => item.collection_id === billing)).toHaveLength(50);
});
