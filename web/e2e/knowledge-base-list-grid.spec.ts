import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

// The article list at desktop width: the column head and every row share the tracks of the columns the person
// chose, the title keeps the free width, and the open button stays at the end of its row.
const base = servers.main;
const headers = { Origin: base };
type Box = { x: number; y: number; width: number; height: number };

const box = async (locator: Locator): Promise<Box> => {
  const found = await locator.boundingBox();
  expect(found, 'the element is on screen').not.toBeNull();
  return found!;
};

async function setColumns(page: Page, columns: string[]) {
  const saved = await page.request.put(`${base}/api/v1/agents/me/kb-list-columns`, { headers, data: { columns } });
  expect(saved.status()).toBe(200);
}

// The px width of each column at 16 px to the rem, from the design's column widths.
const px: Record<string, number> = { status: 120, collection: 144, author: 128, tags: 144, languages: 112, created: 128, updated: 128, votes: 80 };
const names: Record<string, string> = {
  status: 'Status', collection: 'Collection', author: 'Author', tags: 'Tags', languages: 'Languages', created: 'Created', updated: 'Updated', votes: 'Votes',
};

// checkGrid compares the head with the first row. The head and the row show the same first chosen columns (the
// last ones give way when the title would drop under 12rem, and the Columns menu says so); each head label that
// shows starts where its cell starts; the open button sits on the row's line.
async function checkGrid(page: Page, label: string, columns: string[], ordered: boolean) {
  const head = page.locator('.kb-cols-head');
  const row = page.locator('.kb-article-item').filter({ hasText: 'Refund rules' });
  await expect(row).toBeVisible();
  const labels = head.locator('> span');
  const shown = (await labels.count()) - 2 - (ordered ? 1 : 0);
  expect(shown, `${label}: at least the first chosen column shows`).toBeGreaterThanOrEqual(1);
  expect(shown).toBeLessThanOrEqual(columns.length);
  const want = columns.slice(0, shown).map((column) => names[column]);
  expect(await labels.evaluateAll((spans) => spans.slice(1, -1).map((span) => span.textContent ?? '').filter(Boolean)), `${label}: the head shows the first chosen columns`).toEqual(want);
  const link = row.locator('.kb-article-row');
  const cells = link.locator('> *');
  await expect(cells).toHaveCount(shown + 1);
  for (let index = 0; index <= shown; index += 1) {
    if (!(await labels.nth(index).isVisible())) continue;
    const [want, got] = [await box(labels.nth(index)), await box(cells.nth(index))];
    expect.soft(Math.abs(want.x - got.x), `${label}: head label ${index} starts where its cell starts`).toBeLessThanOrEqual(1);
  }
  const title = await box(row.locator('.kb-article-main'));
  // The first chosen column always stays, so with one column the title only has to stay wider than any column.
  if (shown > 1) expect.soft(title.width, `${label}: the title keeps at least 12rem`).toBeGreaterThanOrEqual(191);
  else expect.soft(title.width, `${label}: the title is wider than any fixed column`).toBeGreaterThan(144);
  // A column is left out only when it would not fit: the title has less spare room than that column and its gap.
  if (shown < columns.length) expect.soft(title.width - 192, `${label}: ${columns[shown]} is left out only for want of room`).toBeLessThan(px[columns[shown]] + 16);
  const [rowBox, open] = [await box(link), await box(row.locator('.kb-article-open'))];
  expect.soft(Math.abs(open.y + open.height / 2 - (rowBox.y + rowBox.height / 2)), `${label}: the open button is on the row's line`).toBeLessThanOrEqual(2);
  const item = await box(row);
  expect.soft(Math.round(item.height), `${label}: the row is one line of the grid`).toBeLessThanOrEqual(Math.round(rowBox.height) + 1);
  expect.soft(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), `${label}: no sideways scroll`).toBe(true);
  // The Columns menu says when the list is too narrow for some of the chosen columns.
  const menu = page.locator('.kb-columns-menu');
  await menu.locator('summary').click();
  const note = menu.getByRole('note');
  if (shown < columns.length) await expect(note, `${label}: the menu says columns were left out`).toContainText('too narrow');
  else await expect(note).toHaveCount(0);
  await menu.locator('summary').click();
  return shown;
}

async function seed(page: Page) {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`KB Grid ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${base}/`);
  const { id: billing } = await (await page.request.post(`${base}/api/v1/kb/collections`, { headers, data: { name: 'Billing and invoices' } })).json() as { id: string };
  for (const [title, status] of [['Refund rules', 'published'], ['Change your plan', 'draft']]) {
    expect((await page.request.post(`${base}/api/v1/kb/articles`, { headers,
      data: { collection_id: billing, title, body_md: 'Open Billing.', status, tags: ['billing'] } })).status()).toBe(201);
  }
  return billing;
}

const choices: Record<string, string[]> = {
  'one column': ['status'],
  'the default four': ['status', 'author', 'tags', 'updated'],
  'all eight': ['status', 'collection', 'author', 'tags', 'languages', 'created', 'updated', 'votes'],
};

test.describe('at 1440 px', () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test('kb article list: at desktop width with 1, 4 and 8 columns, with bulk select and in a collection\'s order view, the head lines up with the cells, the title keeps the free width, the open button stays on its row', { tag: '@journey' }, async ({ page }) => {
    const billing = await seed(page);
    for (const [name, columns] of Object.entries(choices)) {
      await setColumns(page, columns);
      // All articles, with the bulk-select tick box an admin gets.
      await page.goto(`${base}/knowledge`);
      await expect(page.locator('.kb-list[data-select="true"]')).toBeVisible();
      await checkGrid(page, `${name}, all articles`, columns, false);
      // One collection: the list is its order, with the move buttons in a column of their own.
      await page.goto(`${base}/knowledge?collection=${billing}`);
      await expect(page.locator('.kb-list[data-order="true"]')).toBeVisible();
      await checkGrid(page, `${name}, the collection's order`, columns, true);
      await page.screenshot({ path: test.info().outputPath(`kb-list-${columns.length}-columns-1440.png`) });
    }
  });
});

test.describe('at 900 px', () => {
  test.use({ viewport: { width: 900, height: 800 } });

  test('kb article list: on a narrow window the head still lines up with the cells and the chosen columns give way in order', { tag: '@journey' }, async ({ page }) => {
    const billing = await seed(page);
    await setColumns(page, choices['all eight']);
    await page.goto(`${base}/knowledge`);
    await checkGrid(page, 'all eight at 900 px, all articles', choices['all eight'], false);
    await page.goto(`${base}/knowledge?collection=${billing}`);
    await expect(page.locator('.kb-list[data-order="true"]')).toBeVisible();
    await checkGrid(page, 'all eight at 900 px, the collection\'s order', choices['all eight'], true);
    await page.screenshot({ path: test.info().outputPath('kb-list-8-columns-900.png') });
  });
});
