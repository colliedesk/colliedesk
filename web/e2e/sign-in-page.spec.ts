import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';

// docs/user/sign-in.md "Create a workspace": the cloud sign-in page as the website's links open it.
// Clerk's own form loads from the network and is not part of this page test.
const baseURL = servers.cloud;

const sidewaysScroll = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);

for (const viewport of [{ width: 1440, height: 900 }, { width: 768, height: 1024 }, { width: 390, height: 844 }]) {
  test(`the trial link titles the page for a new team, with one h1 and no sideways scroll (${viewport.width}px)`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await page.goto(`${baseURL}/sign-in?intent=trial`);
    await expect(page.getByRole('heading', { level: 1, name: 'Start your free trial' })).toBeVisible();
    await expect(page.getByText('Colliedesk is alpha: any release may still change the interface.')).toBeVisible();
    await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1);
    expect(await sidewaysScroll(page)).toBe(0);
  });
}

test('after a Founding link, "Log in" in the same tab opens a sign-in, not Join Founding', async ({ page }) => {
  await page.goto(`${baseURL}/sign-in?buy=founding`);
  await expect(page.getByRole('heading', { level: 1, name: 'Join Founding' })).toBeVisible();
  // The identity provider comes back to the same address: still the buyer's page.
  await page.reload();
  await expect(page.getByRole('heading', { level: 1, name: 'Join Founding' })).toBeVisible();
  await page.goto(`${baseURL}/sign-in`);
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to Colliedesk' })).toBeVisible();
  expect(await page.evaluate(() => window.sessionStorage.getItem('colliedesk.checkout-intent'))).toBeNull();
});
