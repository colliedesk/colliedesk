import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// Settings takes over the sidebar: its own menu under "Back to app", and the sheet holds the page only. Back to
// app, and Esc, return to the page the person came from.
const baseURL = servers.main;

// newDeskWithConversation signs in and opens one conversation, so there is a page to come back to.
async function newDeskWithConversation(page: Page, label: string): Promise<string> {
  const suffix = `${Date.now()}-${process.pid}`;
  await redeemLink(page, createWorkspaceWithSlug(`${label} ${suffix}`, `owner-${suffix}@example.com`).url);
  const headers = { Origin: servers.main };
  const contact = await page.request.post(`${baseURL}/api/v1/contacts`, { headers, data: { name: 'Back Customer', email: `customer-${suffix}@example.com` } });
  const { id: contactId } = await contact.json() as { id: string };
  const created = await page.request.post(`${baseURL}/api/v1/conversations`, { headers, data: { contact_id: contactId, subject: `Come back here ${suffix}` } });
  expect(created.status()).toBe(201);
  const { id } = await created.json() as { id: string };
  return `/conversations/${id}`;
}

test('on a Settings page the sidebar holds the settings menu alone, under Back to app, and the sheet holds the page only', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDeskWithConversation(page, 'E2E settings frame');
  await page.goto(`${baseURL}/settings/general`);
  await expect(page.getByRole('heading', { level: 1, name: 'General' })).toBeVisible();

  // Exactly one landmark lists the settings pages, and it is in the sidebar, not in the sheet.
  const menus = page.locator('nav:has(a[href="/settings/members"])');
  await expect(menus).toHaveCount(1);
  await expect(menus).toHaveAccessibleName('Settings sections');
  await expect(page.locator('.app-sidebar').getByRole('navigation', { name: 'Settings sections' })).toBeVisible();
  await expect(page.locator('main').getByRole('navigation')).toHaveCount(0);

  // The app's places are not offered beside it.
  for (const name of ['Inbox', 'Mentions', 'Knowledge base', 'Reports']) {
    await expect(page.getByRole('link', { name, exact: true }), name).toHaveCount(0);
  }
  await expect(page.getByRole('link', { name: 'Back to app' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'General', exact: true })).toHaveAttribute('aria-current', 'page');

  // The person stays named and reachable.
  await expect(page.getByRole('region', { name: 'Signed in as' })).toBeVisible();
});

test('Back to app returns to the conversation the person came from, and Esc does the same', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const conversation = await newDeskWithConversation(page, 'E2E settings back');
  await page.goto(`${baseURL}${conversation}`);
  await expect(page.getByText(/Come back here/).first()).toBeVisible();

  await page.getByRole('link', { name: 'Settings', exact: true }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/account`);
  await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'General', exact: true }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/general`);
  await page.getByRole('link', { name: 'Back to app' }).click();
  await expect(page).toHaveURL(`${baseURL}${conversation}`);

  await page.getByRole('link', { name: 'Settings', exact: true }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/account`);
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(`${baseURL}${conversation}`);
});

test('with no page before Settings, Back to app opens the inbox', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDeskWithConversation(page, 'E2E settings fallback');
  await page.goto(`${baseURL}/settings/members`);
  await page.getByRole('link', { name: 'Back to app' }).click();
  await expect(page).toHaveURL(`${baseURL}/`);
});

test('Esc leaves Settings, but never while a text field has focus, so typed text is kept', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const conversation = await newDeskWithConversation(page, 'E2E settings esc');
  await page.goto(`${baseURL}${conversation}`);
  await page.getByRole('link', { name: 'Settings', exact: true }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/account`);

  const name = page.getByLabel('Display name');
  await name.click();
  await name.fill('Half typed name');
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(`${baseURL}/settings/account`);
  await expect(name).toHaveValue('Half typed name');

  // Once the focus is out of the field, Esc leaves.
  await page.getByRole('heading', { level: 1, name: 'Account' }).click();
  await name.evaluate((element) => (element as HTMLElement).blur());
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(`${baseURL}${conversation}`);
});

test('the sidebar still folds on a Settings page, and Back to app stays one press away', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDeskWithConversation(page, 'E2E settings fold');
  await page.goto(`${baseURL}/settings/data`);
  await page.getByRole('button', { name: 'Collapse sidebar' }).click();
  await expect(page.getByRole('link', { name: 'Back to app' })).toBeVisible();
  await page.getByRole('button', { name: 'Expand sidebar' }).click();
  await expect(page.getByRole('navigation', { name: 'Settings sections' })).toBeVisible();
});

test.describe('on a phone', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('/settings is the list, and a page shows one back link above its title', async ({ page }) => {
    await newDeskWithConversation(page, 'E2E settings phone');
    await page.goto(`${baseURL}/settings`);
    await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible();
    const menu = page.getByRole('navigation', { name: 'Settings sections' });
    await expect(menu).toHaveCount(1);
    await expect(menu.getByRole('link', { name: 'General', exact: true })).toBeVisible();

    await menu.getByRole('link', { name: 'General', exact: true }).click();
    await expect(page).toHaveURL(`${baseURL}/settings/general`);
    await expect(page.getByRole('navigation', { name: 'Settings sections' })).toHaveCount(0);
    const back = page.getByRole('link', { name: 'All settings' });
    await expect(back).toHaveCount(1);
    await expect(back).toHaveText('Settings');
    const backBox = await back.boundingBox();
    const titleBox = await page.getByRole('heading', { level: 1, name: 'General' }).boundingBox();
    expect(backBox!.y, 'the back link sits above the title').toBeLessThan(titleBox!.y);
    await back.click();
    await expect(page).toHaveURL(`${baseURL}/settings`);
  });

  test('a channel page keeps one back link, not two', async ({ page }) => {
    await newDeskWithConversation(page, 'E2E settings phone channel');
    await page.goto(`${baseURL}/settings/channels/new/web_form`);
    await expect(page.locator('.ch-back')).toBeVisible();
    await expect(page.locator('main .settings-back:visible, main .ch-back:visible')).toHaveCount(1);
  });
});
