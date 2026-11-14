import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// Settings → Members: a row's actions are in its "⋯" menu, moved with the keyboard, and a pending invite can get a new
// link. On a phone the invite form and the email checklist fit the screen.
const baseURL = servers.main;

async function newDesk(page: Page, label: string, viewport = { width: 1440, height: 900 }): Promise<{ email: string }> {
  const suffix = `${Date.now()}-${process.pid}`;
  const email = `owner-${suffix}@example.com`;
  await redeemLink(page, createWorkspaceWithSlug(`${label} ${suffix}`, email).url);
  await page.setViewportSize(viewport);
  return { email };
}

test('a member row opens its menu with Enter or Space, arrows move, Esc closes it, returns the focus and stays on Settings', async ({ page }) => {
  const { email } = await newDesk(page, 'E2E members menu');
  await page.goto(`${baseURL}/settings/members`);
  const trigger = page.getByRole('button', { name: `Actions for ${email}` });
  await expect(trigger).toBeVisible();
  // No inline Change roles, Rename or Deactivate button is left on the row.
  await expect(page.getByRole('button', { name: /^(Change roles|Rename|Deactivate)/ })).toHaveCount(0);

  await trigger.focus();
  await page.keyboard.press('Enter');
  const menu = page.getByRole('menu', { name: `Actions for ${email}` });
  await expect(menu).toBeVisible();
  const items = menu.getByRole('menuitem');
  await expect(items).toHaveText(['Change roles', 'Rename']);
  await expect(items.first()).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(items.nth(1)).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(items.first()).toBeFocused();
  await page.keyboard.press('ArrowUp');
  await expect(items.nth(1)).toBeFocused();

  // Esc closes the menu only: the page stays Settings and the focus is back on the button.
  await page.keyboard.press('Escape');
  await expect(menu).toBeHidden();
  await expect(trigger).toBeFocused();
  await expect(page).toHaveURL(`${baseURL}/settings/members`);

  // Space opens it again; Enter on an action runs it and the form it opens takes the focus.
  await page.keyboard.press('Space');
  await expect(menu).toBeVisible();
  await page.keyboard.press('ArrowDown');
  await page.keyboard.press('Enter');
  const name = page.getByRole('textbox', { name: `Display name of ${email}` });
  await expect(name).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(name).toBeHidden();
  await expect(trigger).toBeFocused();
  await expect(page).toHaveURL(`${baseURL}/settings/members`);
});

test('a pending invite gets a new link: the address and roles are the same, the old link stops, the new one works', async ({ page }) => {
  await newDesk(page, 'E2E new invite link');
  const headers = { Origin: baseURL };
  const first = await (await page.request.post(`${baseURL}/api/v1/auth/invites`, { headers: { ...headers, 'Idempotency-Key': `first-${Date.now()}` }, data: { email: 'kim@example.com' } })).json() as { invite: { id: string }; url: string };
  await page.goto(`${baseURL}/settings/members?tab=invites`);
  const row = page.getByRole('article').filter({ hasText: 'kim@example.com' });
  await row.getByRole('button', { name: 'Actions for kim@example.com' }).click();
  await expect(page.getByRole('menuitem')).toHaveText(['New invite link', 'Revoke']);
  await page.getByRole('menuitem', { name: 'New invite link' }).click();
  const linkField = page.getByRole('textbox', { name: 'One-time invite link' });
  const link = await linkField.inputValue();
  expect(link).not.toBe(first.url);
  // The new link takes the focus and the page says what happened; Esc from there does not leave Settings.
  await expect(linkField).toBeFocused();
  await expect(page.getByText('New invite link made for kim@example.com.')).toHaveAttribute('role', 'status');
  await page.keyboard.press('Escape');
  await expect(page).toHaveURL(`${baseURL}/settings/members?tab=invites`);

  const state = async (url: string) => ((await (await page.request.get(`${baseURL}/api/v1/auth/invite-preview?token=${new URL(url).searchParams.get('token') ?? new URL(url).hash.slice(1)}`)).json()) as { state?: string }).state;
  await expect.poll(() => state(first.url)).toBe('revoked');
  expect(await state(link)).toBe('pending');
  const listed = await (await page.request.get(`${baseURL}/api/v1/auth/invites`)).json() as { items: { id: string; email: string; revoked_at: string | null }[] };
  expect(listed.items.filter((invite) => invite.email === 'kim@example.com' && invite.revoked_at === null)).toHaveLength(1);
  expect(listed.items.find((invite) => invite.id === first.invite.id)?.revoked_at).not.toBeNull();
});

test('on a phone the invite form fits one screen with Create invite at its foot and every control a touch target', async ({ page }) => {
  await newDesk(page, 'E2E phone invite', { width: 390, height: 844 });
  await page.goto(`${baseURL}/settings/members?tab=invites`);
  const email = page.getByRole('textbox', { name: 'Invite email' });
  const roles = page.getByRole('button', { name: /^Roles/ });
  const create = page.getByRole('button', { name: 'Create invite' });
  await expect(email).toBeVisible();
  for (const control of [email, roles, create]) {
    const box = (await control.boundingBox())!;
    expect(box.x, 'inside the screen').toBeGreaterThanOrEqual(0);
    expect(box.x + box.width, 'inside the screen').toBeLessThanOrEqual(390);
    expect(box.y + box.height, 'on the first screen, without scrolling').toBeLessThanOrEqual(844);
    expect(box.height, 'a touch target').toBeGreaterThanOrEqual(44);
  }
  const mail = (await email.boundingBox())!;
  const send = (await create.boundingBox())!;
  expect(send.y, 'Create invite is below the email field').toBeGreaterThan(mail.y);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'no sideways scroll').toBe(true);
  await email.fill('phone-invite@example.com');
  await create.click();
  await expect(page.getByRole('textbox', { name: 'One-time invite link' })).toBeVisible();
  await expect(page.getByRole('article').filter({ hasText: 'phone-invite@example.com' })).toBeVisible();
});

test('on a phone the email checklist is one column and every step is inside the screen', async ({ page }) => {
  await newDesk(page, 'E2E phone email checklist', { width: 390, height: 844 });
  await page.goto(`${baseURL}/settings/channels/type/email`);
  await page.getByRole('textbox', { name: 'Support address' }).fill(`support-${Date.now()}@example.com`);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await expect(page.getByRole('textbox', { name: 'TXT record name' }).or(page.getByRole('button', { name: 'Create inbound secret' }))).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'no sideways scroll').toBe(true);
  const steps = page.locator('[role="group"], section').filter({ has: page.getByRole('heading') });
  const boxes = await Promise.all((await steps.all()).map((step) => step.boundingBox()));
  for (const box of boxes) {
    if (!box || box.width === 0) continue;
    expect(box.x, 'inside the screen').toBeGreaterThanOrEqual(0);
    expect(box.x + box.width, 'inside the screen').toBeLessThanOrEqual(390);
  }
  const headings = await page.getByRole('heading', { level: 2 }).all();
  const lefts = await Promise.all(headings.map(async (heading) => Math.round((await heading.boundingBox())?.x ?? 0)));
  expect(new Set(lefts).size, 'the steps share one column').toBe(1);
});

test('a person who may invite but not revoke gets a new link and is told the earlier link still works', async ({ page, browser }) => {
  await newDesk(page, 'E2E new link no revoke');
  const headers = { Origin: baseURL };
  const suffix = Date.now();
  expect((await page.request.post(`${baseURL}/api/v1/roles`, { headers: { ...headers, 'Idempotency-Key': `inviter-${suffix}` }, data: { name: 'Inviter', permissions: ['team:read', 'team:invite', 'profile:update'] } })).status()).toBe(201);
  const joined = await (await page.request.post(`${baseURL}/api/v1/auth/invites`, { headers: { ...headers, 'Idempotency-Key': `who-${suffix}` }, data: { email: `inviter-${suffix}@example.com`, roles: ['Inviter'] } })).json() as { url: string };
  const context = await browser.newContext();
  const inviter = await context.newPage();
  // A role that reads no conversation lands on Settings, not the inbox: join by hand.
  await inviter.goto(joined.url);
  await inviter.getByRole('button', { name: /^Join / }).click();
  await inviter.waitForLoadState('networkidle');
  await inviter.setViewportSize({ width: 1440, height: 900 });
  const made = await inviter.request.post(`${baseURL}/api/v1/auth/invites`, { headers: { ...headers, 'Idempotency-Key': `kim-${suffix}` }, data: { email: 'kim@example.com', roles: ['Inviter'] } });
  expect(made.status(), await made.text()).toBe(201);
  await inviter.goto(`${baseURL}/settings/members?tab=invites`);
  await inviter.getByRole('button', { name: 'Actions for kim@example.com' }).click();
  await expect(inviter.getByRole('menuitem')).toHaveText(['New invite link']);
  await inviter.getByRole('menuitem', { name: 'New invite link' }).click();
  await expect(inviter.getByText('New link made. The earlier link still works until it expires, or until someone who may revoke invites revokes it.')).toHaveAttribute('role', 'status');
  const listed = await (await inviter.request.get(`${baseURL}/api/v1/auth/invites`)).json() as { items: { email: string; revoked_at: string | null }[] };
  expect(listed.items.filter((invite) => invite.email === 'kim@example.com' && invite.revoked_at === null)).toHaveLength(2);
  await context.close();
});
