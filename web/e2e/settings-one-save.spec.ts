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

test('a rename typed on People survives the Invites tab and comes back open; clicks and arrow keys on a tab list never raise the question', async ({ page }) => {
  await newDesk(page, 'E2E one save arrows');
  await page.goto(`${baseURL}/settings/members`);
  await page.getByRole('button', { name: /^Rename / }).first().click();
  const name = page.getByRole('textbox', { name: /^Display name of / });
  await name.fill('Typed name');
  const asked = answers(page, false);
  await page.getByRole('tab', { name: /^Invites/ }).click();
  await expect(page.getByRole('region', { name: 'Unsaved changes' })).toContainText('Rename');
  await page.getByRole('tab', { name: /^People/ }).focus();
  await page.keyboard.press('ArrowRight');
  await page.getByRole('tab', { name: /^People/ }).click();
  await expect(page.getByRole('textbox', { name: /^Display name of / })).toHaveValue('Typed name');
  expect(asked).toHaveLength(0);
});

test('text typed in a channel tab survives opening another tab of that channel; the menu still asks and keeps it', async ({ page }) => {
  await newDesk(page, 'E2E one save channel tabs');
  const created = await page.request.post(`${baseURL}/api/v1/channels`, { headers: { Origin: baseURL, 'Idempotency-Key': `tabs-${Date.now()}` }, data: { type: 'web_form', config: { allowed_origins: ['https://saved.example.com'] } } });
  expect(created.status(), await created.text()).toBe(201);
  const { id } = await created.json() as { id: string };
  await page.goto(`${baseURL}/settings/channels/${id}?tab=embed`);
  const origins = page.getByRole('textbox', { name: /Allowed origins/ });
  await origins.fill('https://typed.example.com');
  const bar = page.getByRole('region', { name: 'Unsaved changes' });
  await expect(bar).toBeVisible();

  const asked = answers(page, false);
  await page.getByRole('tab', { name: 'History' }).click();
  await expect(origins).toBeHidden();
  await expect(bar).toBeVisible();
  await page.getByRole('tab', { name: 'Embed' }).click();
  await expect(origins).toHaveValue('https://typed.example.com');
  expect(asked).toHaveLength(0);

  await page.getByRole('tab', { name: 'History' }).click();
  await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'General' }).click();
  await expect.poll(() => asked.length).toBe(1);
  await expect(page).toHaveURL(new RegExp(`/settings/channels/${id}`));
  await expect(bar).toBeVisible();
});

test('an invite email typed on Invites is asked about from People: the menu asks, and the draft is still on Invites', async ({ page }) => {
  await newDesk(page, 'E2E one save invite');
  await page.goto(`${baseURL}/settings/members?tab=invites`);
  await page.getByLabel('Invite email').fill('new@example.com');
  const asked = answers(page, false);
  await page.getByRole('tab', { name: /^People/ }).click();
  await expect(page.getByRole('region', { name: 'Unsaved changes' })).toHaveCount(0);
  await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'General' }).click();
  await expect.poll(() => asked.length).toBe(1);
  await expect(page).toHaveURL(/\/settings\/members/);
  await page.getByRole('tab', { name: /^Invites/ }).click();
  await expect(page.getByLabel('Invite email')).toHaveValue('new@example.com');
  // Creating the invite empties the form, and nothing is unsaved afterwards.
  await page.getByRole('button', { name: 'Create invite' }).click();
  await expect(page.getByLabel('Invite email')).toHaveValue('');
  await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'General' }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/general`);
  expect(asked).toHaveLength(1);
});

test('signing out with unsaved text asks once, from the account menu and from the Sessions list; declining keeps the page and the text, agreeing signs out', async ({ page }) => {
  await newDesk(page, 'E2E one save sign out');
  await page.goto(`${baseURL}/settings/account`);
  await page.getByLabel('Display name').fill('Half typed');
  const asked = answers(page, false);
  await page.getByRole('button', { name: 'Account' }).click();
  await page.getByRole('group', { name: 'Account' }).getByRole('button', { name: 'Sign out' }).click();
  await expect.poll(() => asked.length).toBe(1);
  await expect(page.getByLabel('Display name')).toHaveValue('Half typed');
  await page.getByRole('region', { name: 'Sessions' }).getByRole('button', { name: 'Sign out' }).click();
  await expect.poll(() => asked.length).toBe(2);
  await expect(page.getByLabel('Display name')).toHaveValue('Half typed');
  page.removeAllListeners('dialog');
  const leaving = answers(page, true);
  await page.getByRole('region', { name: 'Sessions' }).getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByRole('alert')).toContainText('You are signed out');
  expect(leaving).toHaveLength(1);
});
