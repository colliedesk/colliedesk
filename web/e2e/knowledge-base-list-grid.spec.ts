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
// shows starts where its cell starts; the open button sits on the row's line. `widen` says whether a wider window
// gives the list more room: once the page is at its widest (1020 px of content) the menu only says to pick fewer.
async function checkGrid(page: Page, label: string, columns: string[], ordered: boolean, widen: boolean) {
  const head = page.locator('.kb-cols-head');
  const row = page.locator('.kb-article-item').filter({ hasText: 'Refund rules' });
  await expect(row).toBeVisible();
  const labels = head.locator('> span');
  const shown = (await labels.count()) - 2;
  expect(shown, `${label}: at least the first chosen column shows`).toBeGreaterThanOrEqual(1);
  expect(shown).toBeLessThanOrEqual(columns.length);
  const want = columns.slice(0, shown).map((column) => names[column]);
  expect(await labels.evaluateAll((spans) => spans.slice(1, -1).map((span) => span.textContent ?? '').filter(Boolean)), `${label}: the head shows the first chosen columns`).toEqual(want);
  // Every column the head has must show: a list of 36rem or less hides all but the first, so it must have only that one.
  const visible = await labels.evaluateAll((spans) => spans.slice(1, -1).filter((span) => span.textContent && span.checkVisibility()).length);
  expect.soft(visible, `${label}: every column in the head shows`).toBe(shown);
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
  // A list of 36rem or less shows the title and the first column only (its container query); a wider one leaves
  // a column out only when it would not fit: the title has less spare room than that column and its gap.
  const narrow = await page.locator('.kb-list').evaluate((list) => list.clientWidth <= 36 * parseFloat(getComputedStyle(document.documentElement).fontSize));
  if (narrow) expect.soft(shown, `${label}: a narrow list has the first column only`).toBe(1);
  else if (shown < columns.length) expect.soft(title.width - 192, `${label}: ${columns[shown]} is left out only for want of room`).toBeLessThan(px[columns[shown]] + 16);
  const [rowBox, open] = [await box(link), await box(row.locator('.kb-article-open'))];
  expect.soft(Math.abs(open.y + open.height / 2 - (rowBox.y + rowBox.height / 2)), `${label}: the open button is on the row's line`).toBeLessThanOrEqual(2);
  const item = await box(row);
  expect.soft(Math.round(item.height), `${label}: the row is one line of the grid`).toBeLessThanOrEqual(Math.round(rowBox.height) + 1);
  expect.soft(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), `${label}: no sideways scroll`).toBe(true);
  // The Columns menu says when the list has no room for some of the chosen columns, and what helps.
  const menu = page.locator('.kb-columns-menu');
  await menu.locator('summary').click();
  const note = menu.getByRole('note');
  const left = columns.length - shown;
  const last = left === 1 ? 'column' : `${left} columns`;
  if (left > 0) await expect(note, `${label}: the menu counts the columns left out and says what helps`)
    .toHaveText(widen ? `Too narrow for the last ${last}: widen or pick fewer.` : `No room for the last ${last}: pick fewer.`);
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
  'four columns': ['status', 'author', 'tags', 'updated'],
  'all eight': ['status', 'collection', 'author', 'tags', 'languages', 'created', 'updated', 'votes'],
};

test.describe('at 1440 px', () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test('kb article list: at desktop width with 1, 4 and 8 columns, with bulk select and in a collection\'s order view, the head lines up with the cells, the title keeps the free width, the open button stays on its row', { tag: '@journey' }, async ({ page }) => {
    const billing = await seed(page);
    // A person who never chose sees the default columns, every one of them in All articles: no column left out.
    const defaults = ['status', 'author', 'updated'];
    await page.goto(`${base}/knowledge`);
    await expect(page.locator('.kb-list[data-select="true"]')).toBeVisible();
    expect(await checkGrid(page, 'the default columns, all articles', defaults, false, false)).toBe(3);
    // A collection's order also has its move buttons, beside the open button at the end of the row: the
    // defaults all show there too.
    await page.goto(`${base}/knowledge?collection=${billing}`);
    await expect(page.locator('.kb-list[data-order="true"]')).toBeVisible();
    expect(await checkGrid(page, 'the default columns, the collection\'s order', defaults, true, false)).toBe(3);
    for (const [name, columns] of Object.entries(choices)) {
      await setColumns(page, columns);
      // All articles, with the bulk-select tick box an admin gets.
      await page.goto(`${base}/knowledge`);
      await expect(page.locator('.kb-list[data-select="true"]')).toBeVisible();
      await checkGrid(page, `${name}, all articles`, columns, false, false);
      // One collection: the list is its order, with the move buttons beside the open button.
      await page.goto(`${base}/knowledge?collection=${billing}`);
      await expect(page.locator('.kb-list[data-order="true"]')).toBeVisible();
      await checkGrid(page, `${name}, the collection's order`, columns, true, false);
      await page.screenshot({ path: test.info().outputPath(`kb-list-${columns.length}-columns-1440.png`) });
    }
  });
});

test.describe('on narrower windows', () => {
  test('kb article list: on a narrow window the head still lines up with the cells and the chosen columns give way in order', { tag: '@journey' }, async ({ page }) => {
    const billing = await seed(page);
    // 1280 px: the list (about 690 px) is wider than 36rem, so the fit decides how many columns show.
    await page.setViewportSize({ width: 1280, height: 800 });
    await setColumns(page, choices['all eight']);
    await page.goto(`${base}/knowledge`);
    await checkGrid(page, 'all eight at 1280 px, all articles', choices['all eight'], false, true);
    await page.goto(`${base}/knowledge?collection=${billing}`);
    await expect(page.locator('.kb-list[data-order="true"]')).toBeVisible();
    await checkGrid(page, 'all eight at 1280 px, the collection\'s order', choices['all eight'], true, true);
    // 1150 px: the collections sit beside the list, which is under 36rem and shows the title and the first column
    // only, although two narrow columns would fit.
    await page.setViewportSize({ width: 1150, height: 800 });
    const narrow = ['votes', 'languages', 'status'];
    await setColumns(page, narrow);
    await page.goto(`${base}/knowledge`);
    expect(await checkGrid(page, 'votes, languages, status at 1150 px', narrow, false, true)).toBe(1);
    await page.screenshot({ path: test.info().outputPath('kb-list-narrow.png') });
  });
});

test.describe('on a phone', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('kb article list: on a phone the status shows beside the title whichever column the person put first', { tag: '@journey' }, async ({ page }) => {
    await seed(page);
    await setColumns(page, ['author', 'status']);
    await page.goto(`${base}/knowledge`);
    const row = page.locator('.kb-article-item').filter({ hasText: 'Refund rules' });
    await expect(row.locator('.kb-article-status'), 'the status shows beside the title').toBeVisible();
    await expect(row.locator('.kb-article-status')).toContainText('Published');
    await expect(page.locator('.kb-article-item').filter({ hasText: 'Change your plan' }).locator('.kb-article-status')).toContainText('Draft');
  });
});

test.describe('the languages page at 1440 px', () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test('kb languages: at desktop width the articles of a language keep the title wide and the Translate button on its row', { tag: '@journey' }, async ({ page }) => {
    await seed(page);
    expect((await page.request.put(`${base}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Grid help', locale: 'en' } })).status()).toBe(200);
    expect((await page.request.put(`${base}/api/v1/help-center/languages`, { headers, data: { languages: [{ locale: 'fr', visible: true }] } })).status()).toBe(200);
    await page.goto(`${base}/knowledge/languages?locale=fr&state=missing`);
    const row = page.locator('.kb-language-articles .kb-article-item').filter({ hasText: 'Refund rules' });
    await expect(row).toBeVisible();
    const [item, link, main, status, button] = [await box(row), await box(row.locator('.kb-article-row')),
      await box(row.locator('.kb-article-main')), await box(row.locator('.kb-article-status')), await box(row.locator('.button-link'))];
    // The title takes all the width the status (7.5rem) and the button (8rem) leave, after the row's 16 px padding and two gaps.
    expect.soft(main.width, 'the title takes the free width').toBeGreaterThanOrEqual(item.width - 16 - 120 - 128 - 32 - 2);
    expect.soft(status.x, 'the status sits right after the title').toBeGreaterThanOrEqual(main.x + main.width);
    expect.soft(Math.abs(button.y + button.height / 2 - (link.y + link.height / 2)), 'the Translate button is on the row\'s line').toBeLessThanOrEqual(2);
    expect.soft(button.x + button.width, 'the Translate button ends inside the row').toBeLessThanOrEqual(item.x + item.width);
    expect.soft(Math.round(item.height), 'the row is one line of the grid').toBeLessThanOrEqual(Math.round(link.height) + 1);
    await page.screenshot({ path: test.info().outputPath('kb-languages-1440.png') });
  });
});
