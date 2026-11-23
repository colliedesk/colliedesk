import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// Issue #776. A tab opened before a deploy runs the previous build; the server only has the current build's
// chunks, so the first page part that tab has not loaded yet answers 404. The server here has one build, so the
// test stands the old tab in by answering the Mentions chunk with 404, as the server does for an old name.
const notLoaded = 'This part of the page could not be loaded.';

async function signIn(page: Page) {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Stale Chunk ${suffix}`, `admin-${suffix}@example.com`));
  await expect(page).toHaveURL(`${servers.main}/`);
}

// documentLoads counts full page loads of the app from now on.
function documentLoads(page: Page) {
  const loads: string[] = [];
  page.on('request', (request) => { if (request.resourceType() === 'document') loads.push(new URL(request.url()).pathname); });
  return loads;
}

test('a tab from before a deploy opens Mentions: the app loads again by itself at Mentions and shows it, with no error', { tag: '@journey' }, async ({ page }) => {
  await signIn(page);
  // Until the app loads again, this tab is the old build: every request for the chunk answers 404.
  let oldPage = true;
  let refused = 0;
  page.on('request', (request) => { if (request.resourceType() === 'document') oldPage = false; });
  await page.route('**/assets/mentions-list-*.js', async (route) => {
    if (oldPage) {
      refused += 1;
      await route.fulfill({ status: 404, contentType: 'text/plain', body: '404 page not found' });
      return;
    }
    await route.continue();
  });
  const loads = documentLoads(page);

  const rail = page.getByRole('navigation', { name: 'Main' });
  await rail.getByRole('link', { name: 'Mentions' }).hover();
  await rail.getByRole('link', { name: 'Mentions' }).click();

  await expect(page).toHaveURL(`${servers.main}/mentions`);
  await expect.poll(() => loads).toEqual(['/mentions']);
  // The list itself, which only the Mentions chunk draws.
  await expect(page.getByText(/^No unread mentions\./)).toBeVisible();
  await expect(page.getByText(notLoaded)).toHaveCount(0);
  expect(refused).toBeGreaterThan(0);
});

test('a chunk that still fails after that one reload is shown with Reload the page, and the app never reloads in a loop', { tag: '@journey' }, async ({ page }) => {
  await signIn(page);
  await page.route('**/assets/mentions-list-*.js', (route) => route.fulfill({ status: 404, contentType: 'text/plain', body: '404 page not found' }));
  const loads = documentLoads(page);

  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Mentions' }).click();

  await expect(page.getByText(notLoaded)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reload the page' })).toBeVisible();
  // One reload, made by the app; a moment later still only that one.
  await page.waitForTimeout(1500);
  expect(loads).toEqual(['/mentions']);
});
