import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

const baseURL = servers.main;

// A link to a Settings section lands on it although the sections are drawn only once their data arrives.
// The API answers late here, as over a real network, so the router's own look for the section finds nothing.
test('a link to a settings section lands on it, opened directly or from another settings page', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`E2E section links ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${baseURL}/`);
  await page.route('**/api/v1/**', (route) => setTimeout(() => void route.continue(), 400));

  await page.goto(`${baseURL}/settings#export-heading`);
  await expect(page.getByRole('heading', { name: 'Export all data' })).toBeInViewport();
  await expect(page.getByRole('heading', { name: 'Profile' })).not.toBeInViewport();

  await page.goto(`${baseURL}/settings/ai`);
  await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Sessions', exact: true }).click();
  await expect(page).toHaveURL(`${baseURL}/settings#sessions-heading`);
  await expect(page.getByRole('heading', { name: 'Sessions', exact: true })).toBeInViewport();
  await expect(page.getByRole('heading', { name: 'Profile' })).not.toBeInViewport();

  // The email channel and the web form were sections here; their old anchors open their channel pages.
  await page.goto(`${baseURL}/settings#email-channel-heading`);
  await expect(page).toHaveURL(`${baseURL}/settings/channels/new/email`);
  await expect(page.getByRole('heading', { name: 'Email', exact: true })).toBeVisible();
  await page.goto(`${baseURL}/settings#web-form-heading`);
  await expect(page).toHaveURL(`${baseURL}/settings/channels/new/web_form`);
  await expect(page.getByRole('heading', { name: 'Web form', exact: true })).toBeVisible();
});
