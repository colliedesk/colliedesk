import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// The Settings menu is nine items and one reserved, with no sub-items; Members and AI & agents have tabs in the
// address, and every address a page had before the pages were merged still opens the page that holds it now.
const baseURL = servers.main;

async function newDesk(page: Page, label: string): Promise<void> {
  const suffix = `${Date.now()}-${process.pid}`;
  await redeemLink(page, createWorkspaceWithSlug(`${label} ${suffix}`, `owner-${suffix}@example.com`).url);
}

const at = (page: Page) => { const url = new URL(page.url()); return `${url.pathname}${url.search}${url.hash}`; };

test('the menu lists the nine items and the reserved Conversation fields, with no sub-items, and the reserved page says what is coming', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDesk(page, 'E2E settings menu');
  await page.goto(`${baseURL}/settings/channels`);
  const menu = page.getByRole('navigation', { name: 'Settings sections' });
  await expect(menu.getByRole('link')).toHaveText(['Account', 'General', 'Members', 'Channels', 'Response targets', 'AI & agents', 'Saved replies', 'Conversation fields Coming', 'Data']);
  await expect(menu.getByRole('link', { name: 'Channels' })).toHaveAttribute('aria-current', 'page');
  await expect(menu.locator('ul ul')).toHaveCount(0);
  for (const name of ['Email', 'Web form', 'Help chat', 'Zalo OA']) await expect(menu.getByRole('link', { name, exact: true }), name).toHaveCount(0);

  await menu.getByRole('link', { name: /^Conversation fields/ }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/fields`);
  await expect(page.getByRole('heading', { level: 1, name: 'Conversation fields' })).toBeVisible();
  await expect(page.getByText(/^Coming — imported from Freshdesk ticket fields/)).toBeVisible();
  await expect(menu.getByRole('link', { name: /^Conversation fields/ })).toHaveAttribute('aria-current', 'page');
});

test('every old settings address opens the page, tab or section that holds its content now', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDesk(page, 'E2E settings old links');
  // The expected addresses are the issue's table, with the section or tab each old page's content has now.
  for (const [from, to] of [
    ['/settings/profile', '/settings/account#profile-heading'], ['/settings/preferences', '/settings/account#preferences-heading'], ['/settings/sessions', '/settings/account#sessions-heading'],
    ['/settings/numbers', '/settings/general#numbers-heading'],
    ['/settings/members?tab=roles', '/settings/members?tab=advanced'], ['/settings/roles', '/settings/members?tab=advanced'],
    ['/settings/agents', '/settings/ai?tab=external'],
    ['/settings/import', '/settings/data'], ['/settings/export', '/settings/data#export-heading'], ['/settings/import-freshdesk', '/settings/data'],
    ['/settings/help-center', '/knowledge/settings'],
  ]) {
    await page.goto(`${baseURL}${from}`);
    await expect.poll(() => at(page), from).toBe(to);
    await expect(page.getByRole('heading', { level: 1 }).first(), from).toBeVisible();
  }
  // The server's own links (a refused agent-token call, an old email) are anchors of the one-page Settings.
  await page.goto(`${baseURL}/settings#agents-heading`);
  await expect.poll(() => at(page)).toBe('/settings/ai?tab=external#agents-heading');
  await expect(page.getByRole('heading', { level: 2, name: 'External AI agents' })).toBeVisible();
});

test('Members has the tabs People, Invites, Groups and Advanced in the address, with the roles under Advanced', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDesk(page, 'E2E settings members tabs');
  await page.goto(`${baseURL}/settings/members`);
  const tabs = page.getByRole('tablist', { name: 'Members' });
  await expect(tabs.getByRole('tab')).toHaveText([/^People/, /^Invites/, /^Groups Coming/, 'Advanced']);
  await expect(tabs.getByRole('tab', { name: /^People/ })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('region', { name: 'Team' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Roles' })).toHaveCount(0);

  await tabs.getByRole('tab', { name: 'Advanced' }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/members?tab=advanced`);
  await expect(page.getByRole('region', { name: 'Roles' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Team' })).toHaveCount(0);
  // Admin and Agent are the two built-in roles of a new workspace.
  await expect(page.getByRole('region', { name: 'Roles' }).getByText('Built in')).toHaveCount(2);

  await tabs.getByRole('tab', { name: /^Invites/ }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/members?tab=invites`);
  await expect(page.getByRole('region', { name: 'Invites' })).toBeVisible();

  await tabs.getByRole('tab', { name: /^Groups/ }).click();
  await expect(page.getByRole('tabpanel').getByText(/^Coming — groups/)).toBeVisible();
  // A reload keeps the tab: it is in the address.
  await page.reload();
  await expect(page.getByRole('tab', { name: /^Groups/ })).toHaveAttribute('aria-selected', 'true');
});

// The invite being typed belongs to the page: People and back, and the email and the picked roles are still there.
test('a typed invite email and the picked roles stay when another tab of Members opens and the person comes back', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDesk(page, 'E2E settings invite draft');
  await page.goto(`${baseURL}/settings/members?tab=invites`);
  await page.getByLabel('Invite email').fill('half-typed@example.com');
  await page.getByRole('button', { name: /^Roles: / }).click();
  await page.locator('#invite-roles').getByRole('checkbox', { name: /Admin/ }).check();
  await expect(page.getByRole('button', { name: /^Roles: Admin/ })).toBeVisible();
  await page.getByRole('tab', { name: /^People/ }).click();
  await expect(page.getByRole('region', { name: 'Team' })).toBeVisible();
  await expect(page.getByLabel('Invite email')).toHaveCount(0);
  await page.getByRole('tab', { name: /^Invites/ }).click();
  await expect(page.getByLabel('Invite email')).toHaveValue('half-typed@example.com');
  await expect(page.getByRole('button', { name: /^Roles: Admin/ })).toBeVisible();
  // A click on a tab is a step: Back returns to the tab before.
  await page.goBack();
  await expect(page).toHaveURL(`${baseURL}/settings/members?tab=people`);
});

test('AI & agents has the tabs Built-in AI and External AI agents in the address', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDesk(page, 'E2E settings ai tabs');
  await page.goto(`${baseURL}/settings/ai`);
  const tabs = page.getByRole('tablist', { name: 'AI & agents' });
  await expect(tabs.getByRole('tab')).toHaveText(['Built-in AI', 'External AI agents']);
  await expect(tabs.getByRole('tab', { name: 'Built-in AI' })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('heading', { level: 2, name: 'External AI agents' })).toHaveCount(0);

  await tabs.getByRole('tab', { name: 'External AI agents' }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/ai?tab=external`);
  await expect(page.getByRole('heading', { level: 2, name: 'External AI agents' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Create token' })).toBeVisible();

  await page.getByRole('tab', { name: 'Built-in AI' }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/ai?tab=built-in`);
  await expect(page.getByRole('heading', { level: 2, name: 'External AI agents' })).toHaveCount(0);
});

// A real key press: focus is on the Roles button that opened the picker, not inside the picker. Esc must close the
// picker and neither drop the typed email nor leave Settings.
test('Esc with focus on the Roles button closes the invite role picker and Settings stays on Members', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDesk(page, 'E2E settings esc roles');
  await page.goto(`${baseURL}/settings/members?tab=invites`);
  const email = page.getByLabel('Invite email');
  await email.fill('half-typed@example.com');
  const roles = page.getByRole('button', { name: /^Roles: / });
  await roles.click();
  const picker = page.locator('#invite-roles');
  await expect(picker).toBeVisible();
  await expect(roles).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(picker).toHaveCount(0);
  await expect(roles).toHaveAttribute('aria-expanded', 'false');
  await expect(page).toHaveURL(`${baseURL}/settings/members?tab=invites`);
  await expect(email).toHaveValue('half-typed@example.com');
});

test('on a phone the list of settings shows the new menu, and a page goes back to it', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await newDesk(page, 'E2E settings phone menu');
  await page.goto(`${baseURL}/settings`);
  const list = page.getByRole('navigation', { name: 'Settings sections' });
  await expect(list.getByRole('link')).toHaveText(['Account', 'General', 'Members', 'Channels', 'Response targets', 'AI & agents', 'Saved replies', 'Conversation fields Coming', 'Data']);
  await list.getByRole('link', { name: 'Members', exact: true }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/members`);
  await expect(page.getByRole('tab', { name: /^People/ })).toBeVisible();
  await page.getByRole('link', { name: 'All settings' }).click();
  await expect(page).toHaveURL(`${baseURL}/settings`);
});
