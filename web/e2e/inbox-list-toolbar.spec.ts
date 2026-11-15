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
// The box of an element's words, not of its padding.
const wordsOf = (locator: Locator) => locator.evaluate((element) => {
  const range = document.createRange();
  range.selectNodeContents(element);
  const box = range.getBoundingClientRect();
  return { left: box.left, right: box.right };
});

// The list header (Paper "15 Inbox", I12-a..d): the place the list is in with Search and Filter, then the status and
// the order on one row. The title, the status dot and the rows' dots share one lane; the order's words end over the
// rows' times. A phone (390 px) and the narrowest screen (320 px) show the same header, nothing cut or overflowing.
for (const width of [1440, 1280, 390, 320]) {
  test(`at ${width} px the list header names the place, then the status and the order, in the rows' lanes`, async ({ page }) => {
    await page.setViewportSize({ width, height: width < 600 ? 844 : 900 });
    await inboxWithConversations(page, 3);
    await page.goto(`${servers.main}/`);
    const filters = page.getByRole('search', { name: 'Inbox' });
    const title = filters.getByRole('heading', { level: 2 });
    await expect(title).toHaveText('All channels');
    const rows = page.locator('.conversation-scroll > .conversation-item');
    await expect(rows).toHaveCount(3);
    await expect(page.getByRole('searchbox')).toHaveCount(0);
    const picker = filters.getByRole('button', { name: 'Status: Open' });
    await expect(picker).toContainText('3');
    const sort = filters.locator('.sort-pick');
    // A new workspace has no reply targets, so Due first orders as Waiting longest and says so.
    await expect(sort.locator('.sort-label')).toHaveText('Waiting longest');
    await expect(filters.getByRole('combobox', { name: 'Sort' })).toBeEnabled();

    // Two rows: the title with Search and Filter, then the status and the order.
    const list = await boxOf(page.locator('.list-pane'));
    const titleBox = await boxOf(title);
    const tools = [filters.getByRole('button', { name: 'Search' }), filters.locator('.filter-menu > summary')];
    for (const [index, tool] of (await Promise.all(tools.map(boxOf))).entries()) {
      expect.soft(Math.abs(middle(tool) - middle(titleBox)), `tool ${index} sits on the title's row`).toBeLessThanOrEqual(2);
      expect.soft(tool.x + tool.width, `tool ${index} ends inside the list`).toBeLessThanOrEqual(list.x + list.width);
    }
    const pickerBox = await boxOf(picker);
    const sortBox = await boxOf(sort);
    expect.soft(Math.abs(middle(pickerBox) - middle(sortBox)), 'the order sits on the status row').toBeLessThanOrEqual(2);
    expect.soft(pickerBox.y, 'the status row is under the title').toBeGreaterThanOrEqual(titleBox.y + titleBox.height);

    // The lanes: title, status dot and the rows' dots start together; the order's words end where the rows' times end.
    const rowDot = await boxOf(rows.first().locator('.row-dot'));
    const pickerDot = await boxOf(picker.locator('.status-dot'));
    expect.soft(Math.abs(pickerDot.x - rowDot.x), 'the status dot is in the lane of the rows\' dots').toBeLessThanOrEqual(1);
    expect.soft(Math.abs((await wordsOf(title)).left - rowDot.x), 'the title starts in the lane of the rows\' dots').toBeLessThanOrEqual(1);
    const rowTime = await boxOf(rows.first().locator('.row-top > :is(time, .row-due)').last());
    expect.soft(Math.abs((await wordsOf(sort.locator('.sort-label'))).right - (rowTime.x + rowTime.width)), 'the order ends over the rows\' times').toBeLessThanOrEqual(1);

    // Nothing in the header overflows or is cut, however narrow the list.
    // Every box drawn in the header stays inside it (screen-reader text and closed menus draw none).
    const overflow = await filters.evaluate((root) => {
      const edge = root.getBoundingClientRect();
      const drawn = (element: Element) => !element.closest('.sr-only') && !(element.closest('details:not([open])') && !element.closest('summary'));
      return Array.from(root.querySelectorAll<HTMLElement>('*')).filter(drawn).flatMap((element) => {
        const box = element.getBoundingClientRect();
        if (box.width === 0 || box.height === 0) return [];
        return box.left < edge.left - 0.5 || box.right > edge.right + 0.5 ? [`${element.tagName}.${element.getAttribute('class')}`] : [];
      });
    });
    expect.soft(overflow, 'nothing in the header overflows').toEqual([]);
    expect.soft((await boxOf(filters)).x + (await boxOf(filters)).width, 'the header ends inside the list').toBeLessThanOrEqual(list.x + list.width);
    await page.locator('.list-pane').screenshot({ path: test.info().outputPath(`list-rest-${width}.png`) });

    // The status menu opens under the picker, inside the screen, every status with its count and All statuses last.
    await picker.click();
    const menu = page.getByRole('menu', { name: 'Status' });
    await expect(menu.getByRole('menuitemradio')).toHaveText(['Open3', 'Pending0', 'Resolved0', 'Closed0', 'All statuses3']);
    const menuBox = await boxOf(menu);
    expect.soft(Math.abs(menuBox.x - pickerBox.x), 'the menu lines up with the picker').toBeLessThanOrEqual(1);
    expect.soft(menuBox.x + menuBox.width, 'the menu stays on screen').toBeLessThanOrEqual(width);
    await page.locator('.list-pane').screenshot({ path: test.info().outputPath(`list-status-menu-${width}.png`) });
    await menu.getByRole('menuitemradio', { name: 'Pending' }).click();
    await expect(page).toHaveURL(/status=pending/);
    await expect(filters.getByRole('button', { name: 'Status: Pending' })).toBeFocused();

    // A search in the address (the top bar's "See all results") shows the field in the title's place, scoped.
    await page.goto(`${servers.main}/?search=Question&status=all`);
    const search = page.getByRole('searchbox', { name: 'Search conversations' });
    await expect(search).toHaveValue('Question');
    await expect(filters.getByText('in all channels')).toBeVisible();
    await expect(filters.getByRole('button', { name: 'Status: All statuses' })).toBeVisible();
    const field = await boxOf(filters.locator('.list-search'));
    expect.soft(field.x + field.width, 'the search field ends inside the list').toBeLessThanOrEqual(list.x + list.width);
    await page.locator('.list-pane').screenshot({ path: test.info().outputPath(`list-search-${width}.png`) });

    // A filter shows its count beside Filter and its chip under the status row; the title stays.
    await page.goto(`${servers.main}/?priority=urgent`);
    await expect(title).toHaveText('All channels');
    await expect(page.getByRole('searchbox')).toHaveCount(0);
    await expect(page.getByLabel('1 filter active')).toBeVisible();
    const chip = await boxOf(page.getByRole('list', { name: 'Active filters' }).getByRole('listitem').first());
    expect.soft(chip.y, 'the chips sit under the status row').toBeGreaterThanOrEqual((await boxOf(filters.getByRole('button', { name: /^Status: / }))).y);
    await page.locator('.list-pane').screenshot({ path: test.info().outputPath(`list-filter-${width}.png`) });
  });
}

// Beside an open conversation (I12-a) the list header's bottom line and the conversation header's are one line.
test('at 1440 px the list header and the conversation header end on the same line', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await inboxWithConversations(page, 2);
  await page.goto(`${servers.main}/`);
  await page.locator('.conversation-scroll > .conversation-item').first().locator('.conversation-row').click();
  const thread = await boxOf(page.locator('.timeline-header'));
  const list = await boxOf(page.locator('.list-pane > header'));
  expect.soft(Math.abs((list.y + list.height) - (thread.y + thread.height)), 'the two headers end on one line').toBeLessThanOrEqual(1);
  await page.screenshot({ path: test.info().outputPath('headers-1440.png'), clip: { x: 0, y: 0, width: 1000, height: 220 } });
});
