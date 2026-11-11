import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// Settings is one page per item of its menu (#528, docs/user/workspace.md): each at its own address, Back
// and Forward walk them, and on a phone the menu and a page take turns as the inbox list and a conversation do.
const baseURL = servers.main;

async function newDesk(page: Page, label: string): Promise<void> {
  const suffix = `${Date.now()}-${process.pid}`;
  await redeemLink(page, createWorkspaceWithSlug(`${label} ${suffix}`, `owner-${suffix}@example.com`).url);
}

const noSidewaysScroll = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);

test('on a wide screen the settings menu opens each page at its own address, and Back and Forward walk them', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await newDesk(page, 'E2E settings pages');
  await page.getByRole('link', { name: 'Settings' }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/account`);
  await expect(page.getByRole('heading', { level: 1, name: 'Account' })).toBeVisible();
  const menu = page.getByRole('navigation', { name: 'Settings sections' });
  for (const [label, path, title] of [['General', '/settings/general', 'General'], ['Members', '/settings/members', 'Members'], ['Channels', '/settings/channels', 'Channels'],
    ['Response targets', '/settings/targets', 'Response targets'], ['AI & agents', '/settings/ai', 'AI & agents'], ['Saved replies', '/settings/saved-replies', 'Saved replies'],
    ['Data', '/settings/data', 'Data'], ['Account', '/settings/account', 'Account']]) {
    await menu.getByRole('link', { name: label, exact: true }).click();
    await expect(page).toHaveURL(`${baseURL}${path}`);
    await expect(page.getByRole('heading', { level: 1, name: title })).toBeVisible();
    await expect(menu.getByRole('link', { name: label, exact: true })).toHaveAttribute('aria-current', 'page');
    // The way back to the menu is a phone's; a wide screen shows the menu itself, on every page.
    await expect(page.getByRole('link', { name: 'All settings' }), path).toBeHidden();
  }
  // Each page holds its own sections.
  await expect(page.getByRole('region', { name: 'Sessions' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Invites' })).toHaveCount(0);
  await page.goBack();
  await expect(page).toHaveURL(`${baseURL}/settings/data`);
  await expect(page.getByRole('region', { name: 'Export all data' })).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(`${baseURL}/settings/saved-replies`);
  await page.goForward();
  await expect(page).toHaveURL(`${baseURL}/settings/data`);
  await expect(page.getByRole('heading', { level: 1, name: 'Data' })).toBeVisible();
});

test.describe('on a 390 px phone', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('Settings is the menu, a page opens from it with the way back, and nothing scrolls sideways', async ({ page }) => {
    await newDesk(page, 'E2E settings phone');
    await page.goto(`${baseURL}/settings`);
    await expect(page).toHaveURL(`${baseURL}/settings`);
    const menu = page.getByRole('navigation', { name: 'Settings sections' });
    await expect(menu.getByRole('link', { name: 'Account', exact: true })).toBeVisible();
    await expect(menu.getByText('Data and billing')).toBeVisible();
    expect(await noSidewaysScroll(page), 'the menu fits the phone').toBe(true);
    // Every item of the menu is fingertip-sized.
    for (const link of await menu.getByRole('link').all()) expect((await link.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(44);

    await menu.getByRole('link', { name: 'Members', exact: true }).click();
    await expect(page).toHaveURL(`${baseURL}/settings/members`);
    await expect(page.getByRole('heading', { level: 1, name: 'Members' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Invites' })).toBeVisible();
    await expect(menu).toBeHidden();
    expect(await noSidewaysScroll(page), 'Members fits the phone').toBe(true);

    await page.getByRole('link', { name: 'All settings' }).click();
    await expect(page).toHaveURL(`${baseURL}/settings`);
    await menu.getByRole('link', { name: 'General', exact: true }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'General' })).toBeVisible();
    expect(await noSidewaysScroll(page), 'General fits the phone').toBe(true);
    await page.goBack();
    await expect(page).toHaveURL(`${baseURL}/settings`);
    await expect(menu).toBeVisible();
    await page.goBack();
    await expect(page).toHaveURL(`${baseURL}/settings/members`);
    await expect(page.getByRole('heading', { level: 1, name: 'Members' })).toBeVisible();
  });
});
