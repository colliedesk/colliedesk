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

test.describe('on a phone with touch', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  // A phone scrolls the whole page. The bar stays under the top bar instead of scrolling away with the page.
  test('the workspace name and signed-in address stay on screen after the page scrolls, in the inbox and settings', async ({ page }) => {
    const suffix = `${Date.now()}-${process.pid}`;
    const email = `owner-${suffix}@example.com`;
    const workspace = createWorkspaceWithSlug(`Scrolled Desk ${suffix}`, email);
    await redeemLink(page, workspace.url);
    const headers = { Origin: servers.main };
    const contact = await page.request.post(`${servers.main}/api/v1/contacts`, { headers, data: { name: 'Scroll Customer', email: `customer-${suffix}@example.com` } });
    const { id } = await contact.json() as { id: string };
    for (let number = 1; number <= 20; number += 1) {
      const created = await page.request.post(`${servers.main}/api/v1/conversations`, { headers, data: { contact_id: id, subject: `Scroll ticket ${number}` } });
      expect(created.status()).toBe(201);
    }
    for (const path of ['/', '/settings']) {
      await page.goto(`${servers.main}${path}`);
      await expectIdentityVisible(page, `Scrolled Desk ${suffix}`, email);
      await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
      await expect.poll(() => page.evaluate(() => window.scrollY), `${path} scrolls`).toBeGreaterThan(200);
      await expectIdentityVisible(page, `Scrolled Desk ${suffix}`, email);
      // It sits under the top bar, not behind it.
      const rail = await page.getByRole('navigation', { name: 'Main' }).boundingBox();
      const bar = await page.getByRole('region', { name: 'Signed in as' }).boundingBox();
      expect(bar!.y, `${path}: the bar starts below the top bar`).toBeGreaterThanOrEqual(rail!.y + rail!.height - 1);
    }
  });
});
