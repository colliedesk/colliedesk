import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

const baseURL = servers.main;

// framesWithoutNav counts the frames, from the action until the next page shows, without the Settings nav.
async function framesWithoutNav(page: Page, act: () => Promise<void>, shown: Locator): Promise<number> {
  await page.evaluate(() => {
    const w = window as unknown as { __frames: boolean[]; __stop: boolean };
    w.__frames = [];
    w.__stop = false;
    const tick = () => {
      w.__frames.push(!!document.querySelector('nav.settings-nav'));
      if (!w.__stop) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
  await act();
  await expect(shown).toBeVisible();
  return page.evaluate(() => {
    const w = window as unknown as { __frames: boolean[]; __stop: boolean };
    w.__stop = true;
    return w.__frames.filter((nav) => !nav).length;
  });
}

// Every Settings page is its own chunk. Moving to one not loaded yet keeps the page that is on screen, Settings
// nav included, until the next one can show: the screen never goes blank as if the browser reloaded. Pointing
// at a link loads its page ahead, with what the page reads outside its chunk.
// (routes-never-blank.test.tsx holds every route to the same rule without a browser.)
test('moving between settings pages never blanks the screen, and a link loads its page ahead', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`E2E settings switch ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${baseURL}/`);

  await page.goto(`${baseURL}/settings`);
  const nav = page.getByRole('navigation', { name: 'Settings sections' });
  await expect(nav).toBeVisible();
  // Chunks arrive late, as over a real network.
  await page.route('**/assets/*.js', (route) => setTimeout(() => void route.continue(), 400));

  const blank = await framesWithoutNav(page, () => nav.getByRole('link', { name: 'Response targets', exact: true }).click(),
    page.getByRole('heading', { level: 1, name: 'Response targets' }));
  await expect(page).toHaveURL(`${baseURL}/settings/targets`);
  expect(blank, 'frames without the Settings nav while Response targets loaded').toBe(0);

  // Pointing at Email under Channels asks for the page's chunk and, through the route's loader, what the page
  // reads outside it (the email channel's panels; the channel list, unless it is already there) together.
  const chunk = page.waitForRequest(/\/assets\/channels-route-[^/]+\.js$/);
  const panels = page.waitForRequest(/\/assets\/panels-[^/]+\.js$/, { timeout: 1000 });
  await nav.getByRole('link', { name: 'Email', exact: true }).hover();
  await Promise.all([chunk, panels]);
});
