import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

const baseURL = servers.main;

// framesWithoutNav records, on every frame for a while after an action, whether the Settings nav is on screen.
async function framesWithoutNav(page: Page, act: () => Promise<void>, ms = 1500): Promise<number> {
  await page.evaluate((duration) => {
    const w = window as unknown as { __frames: boolean[] };
    w.__frames = [];
    const start = performance.now();
    const tick = () => {
      w.__frames.push(!!document.querySelector('nav.settings-nav'));
      if (performance.now() - start < duration) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }, ms);
  await act();
  await page.waitForTimeout(ms + 100);
  return page.evaluate(() => (window as unknown as { __frames: boolean[] }).__frames.filter((shown) => !shown).length);
}

// Every Settings page is its own chunk. Moving to one not loaded yet keeps the page that is on screen, Settings
// nav included, until the next one can show: the screen never goes blank as if the browser reloaded.
test('moving between settings pages never blanks the screen, and a hovered link loads its page ahead', async ({ page }) => {
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

  const blankForRoles = await framesWithoutNav(page, () => nav.getByRole('link', { name: 'Roles', exact: true }).click());
  await expect(page).toHaveURL(`${baseURL}/settings/roles`);
  await expect(page.getByRole('heading', { level: 1, name: 'Roles' })).toBeVisible();
  expect(blankForRoles, 'frames without the Settings nav while Roles loaded').toBe(0);

  // A plain link inside a page, followed by the router, keeps the page too.
  const blankForAI = await framesWithoutNav(page, () => page.evaluate(() => {
    const link = Object.assign(document.createElement('a'), { href: '/settings/ai', textContent: 'AI' });
    document.querySelector('main')!.append(link);
    link.click();
  }));
  await expect(page).toHaveURL(`${baseURL}/settings/ai`);
  expect(blankForAI, 'frames without the Settings nav while Built-in AI loaded').toBe(0);

  const targetsChunk = page.waitForRequest(/\/assets\/targets-route-[^/]+\.js$/);
  await nav.getByRole('link', { name: 'Response targets', exact: true }).hover();
  await targetsChunk;
});

// A page that reads data outside its chunk asks for it with the chunk, not after it: hovering Email under
// Channels already asks for the channel list, while the chunk is still on its way.
test('hovering a channel type asks for its chunk and the channel list together', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`E2E settings prefetch ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${baseURL}/`);
  await page.goto(`${baseURL}/settings`);
  const nav = page.getByRole('navigation', { name: 'Settings sections' });
  await expect(nav).toBeVisible();
  await page.route('**/assets/*.js', (route) => setTimeout(() => void route.continue(), 400));

  const chunk = page.waitForRequest(/\/assets\/channels-route-[^/]+\.js$/);
  const list = page.waitForRequest((request) => new URL(request.url()).pathname === '/api/v1/channels' && request.method() === 'GET', { timeout: 1000 });
  await nav.getByRole('link', { name: 'Email', exact: true }).hover();
  await Promise.all([chunk, list]);
});
