import { expect, test } from '@playwright/test';
import type { Dialog, Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// Settings saves one way: a choice saves at once with "Saved · Undo"; typed text waits for one Save (Ctrl/Cmd+S);
// leaving a page with unsaved text asks once, whichever way the person leaves.
const baseURL = servers.main;
const question = 'You have unsaved changes. Leave anyway?';

async function newDesk(page: Page, label: string): Promise<void> {
  const suffix = `${Date.now()}-${process.pid}`;
  await redeemLink(page, createWorkspaceWithSlug(`${label} ${suffix}`, `owner-${suffix}@example.com`).url);
  await page.setViewportSize({ width: 1440, height: 900 });
}

// answer says what the next questions get: false keeps the person on the page, true lets them leave. Each question
// is kept, so a test can say how many were asked.
function answers(page: Page, leave: boolean): Dialog[] {
  const asked: Dialog[] = [];
  page.on('dialog', (dialog) => { asked.push(dialog); void (leave ? dialog.accept() : dialog.dismiss()); });
  return asked;
}

const me = async (page: Page) => (await (await page.request.get(`${baseURL}/api/v1/auth/me`)).json()) as { agent: { display_name: string }; setup_guide_hidden: boolean; timezone: string; workspace: { name: string } };

test('opening another settings page with a typed display name asks once; staying keeps the text and leaving drops it', async ({ page }) => {
  await newDesk(page, 'E2E one save leave');
  await page.goto(`${baseURL}/settings/account`);
  const name = page.getByLabel('Display name');
  await name.fill('Typed but not saved');
  await expect(page.getByRole('region', { name: 'Unsaved changes' })).toContainText('Display name');

  const asked = answers(page, false);
  const menu = page.getByRole('navigation', { name: 'Settings sections' });
  await menu.getByRole('link', { name: 'Response targets' }).click();
  await expect.poll(() => asked.length).toBe(1);
  expect(asked[0].message()).toBe(question);
  await expect(page).toHaveURL(`${baseURL}/settings/account`);
  await expect(name).toHaveValue('Typed but not saved');

  page.removeAllListeners('dialog');
  const leaving = answers(page, true);
  await menu.getByRole('link', { name: 'General' }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/general`);
  expect(leaving).toHaveLength(1);
  await menu.getByRole('link', { name: 'Account' }).click();
  await expect(page.getByLabel('Display name')).not.toHaveValue('Typed but not saved');
  await expect(page.getByRole('region', { name: 'Unsaved changes' })).toHaveCount(0);
  expect((await me(page)).agent.display_name).not.toBe('Typed but not saved');
});

test('Back to app, Esc and the browser Back ask the same question while text is unsaved', async ({ page }) => {
  await newDesk(page, 'E2E one save exits');
  // History made inside the app, so Back is the router's to answer.
  await page.goto(`${baseURL}/settings/general`);
  const menu = page.getByRole('navigation', { name: 'Settings sections' });
  await menu.getByRole('link', { name: 'Account', exact: true }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/account`);
  await page.getByLabel('Display name').fill('Half typed');
  const asked = answers(page, false);

  await page.getByRole('link', { name: 'Back to app' }).click();
  await expect.poll(() => asked.length).toBe(1);
  await expect(page).toHaveURL(`${baseURL}/settings/account`);

  // Esc is for leaving only from the page itself: click empty page space, then press it.
  await page.getByRole('heading', { level: 1, name: 'Account' }).click();
  await page.locator('body').press('Escape');
  await expect.poll(() => asked.length).toBe(2);
  await expect(page).toHaveURL(`${baseURL}/settings/account`);

  // The browser's own Back button: page.goBack() waits for a navigation that the person has just declined.
  await page.evaluate(() => history.back());
  await expect.poll(() => asked.length).toBe(3);
  await expect(page).toHaveURL(`${baseURL}/settings/account`);
  await expect(page.getByLabel('Display name')).toHaveValue('Half typed');
  expect(asked.every((dialog) => dialog.message() === question)).toBe(true);
});

test('closing the tab with unsaved text asks the browser to confirm', async ({ page }) => {
  await newDesk(page, 'E2E one save unload');
  await page.goto(`${baseURL}/settings/account`);
  await page.getByLabel('Display name').fill('Not saved yet');
  const unload = page.waitForEvent('dialog');
  await page.close({ runBeforeUnload: true });
  expect((await unload).type()).toBe('beforeunload');
});

test('Ctrl+S saves the typed name, the bar goes, and the server holds it; Discard drops the typing', async ({ page }) => {
  await newDesk(page, 'E2E one save key');
  await page.goto(`${baseURL}/settings/account`);
  const name = page.getByLabel('Display name');
  await name.fill('Saved with the key');
  await page.getByLabel('Display name').press('Control+s');
  await expect(page.getByRole('region', { name: 'Unsaved changes' })).toHaveCount(0);
  await expect.poll(async () => (await me(page)).agent.display_name).toBe('Saved with the key');

  await name.fill('Changed my mind');
  await page.getByRole('region', { name: 'Unsaved changes' }).getByRole('button', { name: 'Discard' }).click();
  await expect(name).toHaveValue('Saved with the key');
  await expect(page.getByRole('region', { name: 'Unsaved changes' })).toHaveCount(0);
});

test('the bar on General names the workspace name, and Save keeps it', async ({ page }) => {
  await newDesk(page, 'E2E one save general');
  await page.goto(`${baseURL}/settings/general`);
  await page.getByLabel('Workspace name').fill('Northwind Support');
  const bar = page.getByRole('region', { name: 'Unsaved changes' });
  await expect(bar).toContainText('Workspace name');
  await bar.getByRole('button', { name: 'Save' }).click();
  await expect(bar).toHaveCount(0);
  await expect.poll(async () => (await me(page)).workspace.name).toBe('Northwind Support');
});

test('a toggle saves at once with Saved and Undo, and Undo restores the previous value', async ({ page }) => {
  await newDesk(page, 'E2E one save toggle');
  await page.goto(`${baseURL}/settings/account`);
  const guide = page.getByRole('checkbox', { name: 'Show the setup guide' });
  await expect(guide).toBeChecked();
  expect((await me(page)).setup_guide_hidden).toBe(false);

  await guide.click();
  const toast = page.getByRole('status').filter({ hasText: 'Saved' });
  await expect(toast).toBeVisible();
  await expect.poll(async () => (await me(page)).setup_guide_hidden).toBe(true);
  // A choice is not text: no save bar for it.
  await expect(page.getByRole('region', { name: 'Unsaved changes' })).toHaveCount(0);

  await toast.getByRole('button', { name: 'Undo' }).click();
  await expect.poll(async () => (await me(page)).setup_guide_hidden).toBe(false);
  await expect(guide).toBeChecked();
  await expect(toast).toHaveCount(0);
});

test('a timezone picked from the list saves at once and Undo puts the previous one back', async ({ page }) => {
  await newDesk(page, 'E2E one save timezone');
  await page.goto(`${baseURL}/settings/account`);
  const before = (await me(page)).timezone;
  await page.getByLabel('Your timezone').selectOption({ label: 'Asia/Tokyo' });
  await expect.poll(async () => (await me(page)).timezone).toBe('Asia/Tokyo');
  await page.getByRole('status').filter({ hasText: 'Saved' }).getByRole('button', { name: 'Undo' }).click();
  await expect.poll(async () => (await me(page)).timezone).toBe(before);
});
