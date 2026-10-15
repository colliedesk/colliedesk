import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { expectIdentityVisible, redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// The workspace name and address are the guard against acting in the wrong workspace after
// following a link. They must be readable on screen, not hidden behind a hover tooltip.
test('the workspace name and signed-in address stay visible on wide and narrow screens, in the inbox and settings', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const email = `owner-${suffix}@example.com`;
  const workspace = createWorkspaceWithSlug(`Visible Desk ${suffix}`, email);
  await redeemLink(page, workspace.url);
  for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    await page.goto(`${servers.main}/`);
    await expectIdentityVisible(page, `Visible Desk ${suffix}`, email);
    await page.goto(`${servers.main}/settings`);
    await expectIdentityVisible(page, `Visible Desk ${suffix}`, email);
  }
});
