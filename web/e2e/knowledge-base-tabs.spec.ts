import { expect, test } from '@playwright/test';
import { servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// The knowledge base tabs are one row. Where the screen is too narrow for all of them, the row scrolls
// inside itself: the page never scrolls sideways, and the tab of the page you are on is on screen.
for (const width of [390, 720, 768]) {
  test(`at ${width} px every knowledge base page shows its own tab, and the page does not scroll sideways`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 });
    const suffix = uniqueSuffix();
    await redeemLink(page, createWorkspaceWithSlug(`Tabs Desk ${suffix}`, `owner-${suffix}@example.com`).url);
    for (const [path, tab] of [['/knowledge', 'Articles'], ['/knowledge/templates', 'Templates'], ['/knowledge/searches', 'Searches'], ['/knowledge/settings', 'Settings']]) {
      await page.goto(`${servers.main}${path}`);
      const current = page.getByRole('navigation', { name: 'Knowledge base sections' }).locator('a[aria-current="page"]');
      await expect(current, `${path}: one tab is the current page`).toContainText(tab);
      await page.waitForTimeout(300);
      const seen = await current.evaluate((link) => {
        const box = link.getBoundingClientRect();
        const strip = link.parentElement!.getBoundingClientRect();
        return { left: box.left, right: box.right, stripLeft: strip.left, stripRight: strip.right, page: document.documentElement.scrollWidth, screen: window.innerWidth };
      });
      expect(seen.page, `${path}: the page is no wider than the screen`).toBeLessThanOrEqual(seen.screen);
      expect(seen.left, `${path}: the current tab starts inside the row`).toBeGreaterThanOrEqual(seen.stripLeft - 1);
      expect(seen.right, `${path}: the current tab ends inside the row`).toBeLessThanOrEqual(seen.stripRight + 1);
    }
    await page.screenshot({ path: test.info().outputPath(`kb-tabs-${width}.png`) });
  });
}
