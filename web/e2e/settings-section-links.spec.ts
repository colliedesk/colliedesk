import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

const baseURL = servers.main;

// Settings was one page with a section per anchor; each section now lives on the page of its menu item (#528).
// A link to an old anchor (a bookmark, a sent email, an MCP answer) opens that page at the section, although
// the sections are drawn only once their data arrives. The API answers late here, as over a real network,
// so the router's own look for the section finds nothing.
test('a link to an old settings section opens the page that holds it now and lands on the section', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`E2E section links ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${baseURL}/`);
  await page.route('**/api/v1/**', (route) => setTimeout(() => void route.continue(), 400));

  // Data is longer than this window: the import's form comes first, Export all data after it.
  await page.setViewportSize({ width: 1280, height: 600 });
  await page.goto(`${baseURL}/settings#export-heading`);
  await expect(page).toHaveURL(`${baseURL}/settings/data#export-heading`);
  await expect(page.getByRole('heading', { name: 'Export all data' })).toBeInViewport();
  await expect(page.getByRole('heading', { name: 'Data', level: 1 })).not.toBeInViewport();
  await page.setViewportSize({ width: 1280, height: 800 });

  await page.goto(`${baseURL}/settings#sessions-heading`);
  await expect(page).toHaveURL(`${baseURL}/settings/account#sessions-heading`);
  await expect(page.getByRole('heading', { name: 'Sessions', exact: true })).toBeInViewport();

  // A refused token call (the server's `where`) and an MCP answer link the tokens; the roles editor links their sessions.
  // AI & agents starts with the built-in AI, which is drawn from a request of its own and pushes them down.
  await page.setViewportSize({ width: 1280, height: 600 });
  await page.goto(`${baseURL}/settings#agents-heading`);
  await expect(page).toHaveURL(`${baseURL}/settings/ai?tab=external#agents-heading`);
  // The section's name is kept for the screen reader and for this anchor, not drawn again under its tab: the section is what lands.
  await expect(page.getByRole('region', { name: 'External AI agents' })).toBeInViewport();
  await expect(page.getByRole('heading', { name: 'AI & agents', level: 1 })).not.toBeInViewport();
  await page.goto(`${baseURL}/settings/ai#agent-tokens-heading`);
  await expect(page).toHaveURL(`${baseURL}/settings/ai?tab=external#agent-tokens-heading`);
  await expect(page.locator('#agent-tokens-heading')).toBeInViewport();
  await expect(page.getByRole('heading', { name: 'AI & agents', level: 1 })).not.toBeInViewport();
  await page.setViewportSize({ width: 1280, height: 800 });

  await page.goto(`${baseURL}/settings#invites-heading`);
  await expect(page).toHaveURL(`${baseURL}/settings/members?tab=invites#invites-heading`);
  await expect(page.getByRole('heading', { name: 'Invites', exact: true })).toBeVisible();

  // The email channel and the web form were sections here; their old anchors open their pages in Settings.
  await page.goto(`${baseURL}/settings#email-channel-heading`);
  await expect(page).toHaveURL(`${baseURL}/settings/channels/type/email`);
  await expect(page.getByRole('heading', { name: 'Connect your support email' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Email', exact: true })).toBeVisible();
  await page.goto(`${baseURL}/settings#web-form-heading`);
  await expect(page).toHaveURL(`${baseURL}/settings/channels/new/web_form`);
  await expect(page.getByRole('heading', { name: 'Web form', exact: true })).toBeVisible();

  // The help center's settings moved to the Help center area.
  await page.goto(`${baseURL}/settings#help-center-heading`);
  await expect(page).toHaveURL(`${baseURL}/knowledge/settings#help-center-heading`);
  await expect(page.getByRole('heading', { name: 'Help center', exact: true })).toBeVisible();
});
