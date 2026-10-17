import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// docs/user/sessions.md: the avatar menu signs this browser out, on wide and narrow screens.
for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
  test(`sign out from the account menu ends the session (${viewport.width}px)`, async ({ page }) => {
    const suffix = `${Date.now()}-${process.pid}-${viewport.width}`;
    const email = `owner-${suffix}@example.com`;
    await page.setViewportSize(viewport);
    await redeemLink(page, createWorkspaceWithSlug(`Sign Out Desk ${suffix}`, email).url);
    await page.getByRole('button', { name: 'Account' }).click();
    await expect(page.getByRole('group', { name: 'Account' })).toContainText(email);
    await page.getByRole('button', { name: 'Sign out' }).click();
    await expect(page.getByRole('alert')).toContainText('You are signed out');
    await expect(page.getByRole('link', { name: 'Settings' })).toHaveCount(0);
    await page.goto(`${servers.main}/settings`);
    await expect(page.getByRole('alert')).toContainText('You are signed out');
  });
}
