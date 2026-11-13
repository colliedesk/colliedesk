import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

async function inboxWithConversations(page: Page, count: number): Promise<void> {
  const suffix = `${Date.now()}-${process.pid}`;
  await redeemLink(page, createWorkspaceWithSlug(`Toolbar Desk ${suffix}`, `owner-${suffix}@example.com`).url);
  const headers = { Origin: servers.main };
  for (let index = 0; index < count; index++) {
    const contact = await page.request.post(`${servers.main}/api/v1/contacts`, { headers, data: { name: `Customer ${index}`, email: `toolbar-${index}-${suffix}@example.com` } });
    expect(contact.status()).toBe(201);
    const { id } = await contact.json() as { id: string };
    const created = await page.request.post(`${servers.main}/api/v1/conversations`, { headers, data: { contact_id: id, subject: `Question ${index}` } });
    expect(created.status()).toBe(201);
  }
}

type Box = { x: number; y: number; width: number; height: number };
const boxOf = async (locator: Locator): Promise<Box> => {
  const found = await locator.boundingBox();
  expect(found, 'the element is laid out').not.toBeNull();
  return found!;
};
const middle = (box: Box) => box.y + box.height / 2;

// The tools on the right of the list's first row, in the order a person reads and tabs through them.
const toolsOf = (page: Page) => [page.locator('.list-pane .sort-pick'), page.locator('.list-pane .filter-menu > summary')];

// At rest the list starts on one row: the status tabs on the left, the tools on the
// right, inside the list column. A search or a filter brings the search row above the tabs.
// A phone (390 px) shows the list alone, with the same one row.
for (const width of [1440, 1280, 390]) {
  test(`at ${width} px the status tabs and the tools share one row, and a search brings its own row above the tabs`, async ({ page }) => {
    await page.setViewportSize({ width, height: width < 600 ? 844 : 900 });
    await inboxWithConversations(page, 3);
    await page.goto(`${servers.main}/`);
    const status = page.getByRole('radiogroup', { name: 'Status' });
    await expect(status.getByRole('radio')).toHaveCount(3);
    await expect(page.locator('.conversation-scroll > .conversation-item')).toHaveCount(3);
    await expect(page.getByRole('searchbox')).toHaveCount(0);

    const list = await boxOf(page.locator('.list-pane'));
    const tabs = await boxOf(status);
    const tools = await Promise.all(toolsOf(page).map(boxOf));
    for (const [index, tool] of tools.entries()) {
      expect.soft(Math.abs(middle(tool) - middle(tabs)), `tool ${index} sits on the row of the tabs`).toBeLessThanOrEqual(4);
      expect.soft(tool.x, `tool ${index} starts inside the list`).toBeGreaterThanOrEqual(list.x);
      expect.soft(tool.x + tool.width, `tool ${index} ends inside the list`).toBeLessThanOrEqual(list.x + list.width);
    }
    for (let index = 1; index < tools.length; index++) {
      expect.soft(tools[index].x, `tool ${index} comes after tool ${index - 1} without overlap`).toBeGreaterThanOrEqual(tools[index - 1].x + tools[index - 1].width);
    }
    const lastWord = await status.evaluate((row) => {
      const labels = Array.from(row.querySelectorAll('label'));
      const last = labels[labels.length - 1].querySelectorAll('span');
      const range = document.createRange();
      range.setStartBefore(last[0]);
      range.setEndAfter(last[last.length - 1]);
      return range.getBoundingClientRect().right;
    });
    // The words never run under the icons, and the three tabs of an ordinary day fit without scrolling.
    expect.soft(lastWord, 'the last tab word ends before the tools').toBeLessThanOrEqual(tools[0].x);
    expect.soft(await status.evaluate((row) => row.scrollWidth <= row.clientWidth), 'the three tabs fit beside the tools').toBe(true);
    // The row is one line: the header holds the tabs (36 px, 44 on a phone) and 4 px above them, no second row.
    const header = await boxOf(page.locator('.list-pane > header'));
    expect.soft(header.height, 'the list header is one row').toBeLessThanOrEqual(tabs.height + 5);
    await page.locator('.list-pane').screenshot({ path: test.info().outputPath(`list-rest-${width}.png`) });

    // A search in the address (the top bar's "See all results") shows the search row with its words.
    await page.goto(`${servers.main}/?search=Question&status=all`);
    const search = page.getByRole('searchbox', { name: 'Search conversations' });
    await expect(search).toHaveValue('Question');
    await expect(status.getByRole('radio')).toHaveCount(5);
    const field = await boxOf(search);
    const tabsBelow = await boxOf(status);
    expect.soft(field.y + field.height, 'the search box sits above the tabs').toBeLessThanOrEqual(tabsBelow.y);
    expect.soft(field.x + field.width, 'the search box ends inside the list').toBeLessThanOrEqual(list.x + list.width);
    await page.locator('.list-pane').screenshot({ path: test.info().outputPath(`list-search-${width}.png`) });

    // A filter shows the search row with the Filter count, and its chip below the tabs.
    await page.goto(`${servers.main}/?priority=urgent`);
    await expect(page.getByRole('searchbox', { name: 'Search conversations' })).toHaveValue('');
    await expect(page.getByLabel('1 filter active')).toBeVisible();
    const chips = await boxOf(page.getByRole('list', { name: 'Active filters' }).getByRole('listitem').first());
    expect.soft(chips.y, 'the chips sit below the tabs').toBeGreaterThanOrEqual((await boxOf(status)).y);
    await page.locator('.list-pane').screenshot({ path: test.info().outputPath(`list-filter-${width}.png`) });
  });
}
