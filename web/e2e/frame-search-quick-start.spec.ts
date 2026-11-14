import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// The frame of every page (Paper "15 Inbox"): one search field in the middle of the top bar that opens the search
// palette, and Quick start beside the workspace name in the sidebar head, whose checklist opens under it.
const base = servers.main;
const headers = { Origin: base };

async function deskWithConversations(page: Page, name: string): Promise<string> {
  const suffix = `${Date.now()}-${process.pid}`;
  const workspace = `${name} ${suffix}`;
  await redeemLink(page, createWorkspaceWithSlug(workspace, `owner-${suffix}@example.com`).url);
  const contact = await page.request.post(`${base}/api/v1/contacts`, { headers, data: { name: 'Hana Sato', email: `hana-${suffix}@kumo.example` } });
  expect(contact.status()).toBe(201);
  const { id } = await contact.json() as { id: string };
  for (const subject of ['Refund still not on my card', 'Invoice copy for September']) {
    const created = await page.request.post(`${base}/api/v1/conversations`, { headers, data: { contact_id: id, subject } });
    expect(created.status()).toBe(201);
  }
  return workspace;
}

const box = async (locator: Locator) => {
  const found = await locator.boundingBox();
  expect(found, 'the element is on screen').not.toBeNull();
  return found!;
};
const noSidewaysScroll = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);

test.describe('at 1440 px', () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test('the search field sits in the top bar, Ctrl K opens the palette over it, and its rows open conversations and the inbox search', async ({ page }) => {
    const workspace = await deskWithConversations(page, 'Frame Desk');
    await page.goto(`${base}/`);
    await expect(page.locator('.conversation-scroll > .conversation-item')).toHaveCount(2);
    const bar = page.locator('.app-topbar');
    const field = page.getByRole('banner').getByRole('button', { name: `Search ${workspace}` });
    await expect(field).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('button', { name: /Search/ })).toHaveCount(0);
    const fieldBox = await box(field);
    const barBox = await box(bar);
    expect.soft(Math.round(fieldBox.height), 'the field is 32 px tall, the one control height').toBe(32);
    expect.soft(Math.round(fieldBox.width), 'the field is 540 px wide at 1440 px').toBe(540);
    expect.soft(fieldBox.y >= barBox.y && fieldBox.y + fieldBox.height <= barBox.y + barBox.height, 'the field is inside the top bar').toBe(true);
    expect.soft(await noSidewaysScroll(page), 'no sideways scroll').toBe(true);
    await page.screenshot({ path: test.info().outputPath('frame-1440.png') });

    // Ctrl K from the page opens the palette, centred on the field and inside the window.
    await page.keyboard.press('Control+k');
    const palette = page.getByRole('dialog', { name: 'Search' });
    const input = palette.getByRole('combobox', { name: 'Search conversations, customers and pages' });
    await expect(input).toBeFocused();
    const paletteBox = await box(palette);
    expect.soft(Math.round(paletteBox.width), 'the palette is 640 px wide').toBe(640);
    expect.soft(Math.abs(paletteBox.x + paletteBox.width / 2 - (fieldBox.x + fieldBox.width / 2)), 'the palette is centred on the field').toBeLessThanOrEqual(1);
    expect.soft(paletteBox.x >= 0 && paletteBox.x + paletteBox.width <= 1440 && paletteBox.y >= 0 && paletteBox.y + paletteBox.height <= 900, 'the palette is inside the window').toBe(true);
    await expect(palette.getByRole('group', { name: 'Go to' })).toBeVisible();
    await page.screenshot({ path: test.info().outputPath('palette-empty-1440.png') });

    await input.fill('hana');
    const conversations = palette.getByRole('group', { name: 'Conversations' });
    await expect(conversations.getByRole('option')).toHaveCount(2);
    await expect(palette.getByRole('group', { name: 'Customers' }).getByRole('option')).toHaveCount(1);
    await expect(conversations.getByRole('option').first()).toHaveAttribute('aria-selected', 'true');
    await expect(palette.getByRole('option', { name: /See all results for “hana” in the inbox/ })).toContainText('2 conversations');
    await page.screenshot({ path: test.info().outputPath('palette-typed-1440.png') });

    // Enter opens the first row; the palette closes and the conversation is open.
    const first = (await conversations.getByRole('option').first().locator('.palette-name').textContent())!;
    await page.keyboard.press('Enter');
    await expect(palette).toHaveCount(0);
    await expect(page).toHaveURL(/\/conversations\/[0-9a-f-]{36}/);
    await expect(page.locator('.conversation-center')).toContainText(first);

    // Opened, it heads Recent; Escape closes and gives the focus back to the field that opened it.
    await field.click();
    await expect(palette.getByRole('group', { name: 'Recent' }).getByRole('option').first()).toContainText(first);
    await page.keyboard.press('Escape');
    await expect(palette).toHaveCount(0);
    await expect(field).toBeFocused();

    // See all results opens the inbox list searched in every status.
    await page.keyboard.press('Control+k');
    await input.fill('hana');
    await palette.getByRole('option', { name: /See all results/ }).click();
    await expect(page).toHaveURL(/\/\?(.*&)?search=hana(&|$)/);
    expect(new URL(page.url()).searchParams.get('status')).toBe('all');
  });

  test('Quick start opens its checklist under it, inside the list column, and nothing floats over the list while it is closed', async ({ page }) => {
    await deskWithConversations(page, 'Quick Desk');
    await page.goto(`${base}/`);
    const rows = page.locator('.conversation-scroll > .conversation-item');
    await expect(rows).toHaveCount(2);
    const list = page.locator('.list-pane');
    const listBox = await box(list);
    // Closed, nothing covers the list: the point at its foot is the list's own.
    const covered = await page.evaluate(({ x, y }) => !document.elementFromPoint(x, y)?.closest('.list-pane'), { x: listBox.x + 24, y: listBox.y + listBox.height - 24 });
    expect.soft(covered, 'nothing floats over the foot of the list').toBe(false);
    await expect(page.getByText('Finish setting up')).toHaveCount(0);

    const head = page.locator('.rail-head');
    const button = head.getByRole('button', { name: /^Quick start, \d+ steps? left$/ });
    await expect(button).toBeVisible();
    await expect(head.getByRole('button', { name: 'New conversation' })).toBeVisible();
    await expect(button.locator('.quick-start-dot')).toBeVisible();
    await button.click();
    await expect(button).toHaveAttribute('aria-pressed', 'true');
    const popover = page.getByRole('dialog', { name: /^Get .* ready$/ });
    await expect(popover.getByRole('heading', { level: 3, name: 'Set up' })).toBeVisible();
    const buttonBox = await box(button);
    const popoverBox = await box(popover);
    expect.soft(popoverBox.y, 'the checklist opens under the icon').toBeGreaterThanOrEqual(buttonBox.y + buttonBox.height);
    expect.soft(popoverBox.x + popoverBox.width, 'the checklist ends inside the list column, never over the conversation').toBeLessThanOrEqual(listBox.x + listBox.width + 0.5);
    expect.soft(popoverBox.y + popoverBox.height, 'the checklist ends inside the window').toBeLessThanOrEqual(900);
    await page.screenshot({ path: test.info().outputPath('quick-start-1440.png') });
    await page.keyboard.press('Escape');
    await expect(popover).toHaveCount(0);
    await expect(button).toBeFocused();
  });
});

test.describe('at 390 px', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('search is an icon in the top bar, Quick start and the pen sit in the top row, and nothing scrolls sideways', async ({ page }) => {
    const workspace = await deskWithConversations(page, 'Phone Frame');
    await page.goto(`${base}/`);
    await expect(page.locator('.conversation-scroll > .conversation-item')).toHaveCount(2);
    const search = page.getByRole('banner').getByRole('button', { name: `Search ${workspace}` });
    const searchBox = await box(search);
    expect.soft(Math.round(searchBox.width), 'the search icon is a fingertip wide').toBeGreaterThanOrEqual(44);
    expect.soft(Math.round(searchBox.height), 'the search icon is a fingertip tall').toBeGreaterThanOrEqual(44);
    const rail = page.getByRole('navigation', { name: 'Main' });
    const quickStart = rail.getByRole('button', { name: /^Quick start/ });
    await expect(quickStart).toBeVisible();
    await expect(rail.getByRole('button', { name: 'New conversation' })).toBeVisible();
    expect.soft(await noSidewaysScroll(page), 'no sideways scroll').toBe(true);
    await page.screenshot({ path: test.info().outputPath('frame-390.png') });

    await search.click();
    const palette = page.getByRole('dialog', { name: 'Search' });
    await expect(palette.getByRole('combobox')).toBeFocused();
    const paletteBox = await box(palette);
    expect.soft(paletteBox.x >= 0 && paletteBox.x + paletteBox.width <= 390, 'the palette is inside the window').toBe(true);
    await palette.getByRole('combobox').fill('hana');
    await expect(palette.getByRole('group', { name: 'Conversations' }).getByRole('option')).toHaveCount(2);
    expect.soft(await noSidewaysScroll(page), 'no sideways scroll with the palette open').toBe(true);
    await page.screenshot({ path: test.info().outputPath('palette-typed-390.png') });
    await page.keyboard.press('Escape');

    await quickStart.click();
    const popover = page.getByRole('dialog', { name: /^Get .* ready$/ });
    await expect(popover.getByRole('heading', { level: 3, name: 'Set up' })).toBeVisible();
    const popoverBox = await box(popover);
    expect.soft(popoverBox.x >= 0 && popoverBox.x + popoverBox.width <= 390, 'the checklist is inside the window').toBe(true);
    expect.soft(await noSidewaysScroll(page), 'no sideways scroll with Quick start open').toBe(true);
    await page.screenshot({ path: test.info().outputPath('quick-start-390.png') });
  });
});
