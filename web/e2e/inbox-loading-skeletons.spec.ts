import { expect, test } from '@playwright/test';
import type { Locator, Page, Route } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// While the list or a timeline loads, grey blocks in the shape of its rows and messages hold the pane. They are
// drawn with the classes of the real rows, so when the data arrives nothing moves: the first row and the first
// message start where their blocks were, and a row is as tall as its block.
const base = servers.main;
const headers = { Origin: base };

async function conversations(page: Page, count: number): Promise<string[]> {
  const site = 'https://skeletons.example';
  const form = await page.request.post(`${base}/api/v1/channels`, { headers, data: { type: 'web_form', name: 'Website form', config: { allowed_origins: [site] } } });
  expect(form.status()).toBe(201);
  const path = (await form.json() as { config: { submit_path: string } }).config.submit_path;
  for (let index = 0; index < count; index++) {
    const posted = await page.request.post(`${base}${path}`, { headers: { Origin: site, 'Content-Type': 'application/json' },
      data: { name: `Customer ${index}`, email: `customer-${index}-${Date.now()}@example.com`, message: `Question number ${index}: the export button does nothing on Safari.` } });
    expect(posted.status()).toBe(202);
  }
  let ids: string[] = [];
  await expect.poll(async () => {
    ids = (await (await page.request.get(`${base}/api/v1/conversations?page_size=50`)).json() as { items: Array<{ id: string }> }).items.map((item) => item.id);
    return ids.length;
  }, { timeout: 30_000 }).toBeGreaterThanOrEqual(count);
  return ids;
}

// hold keeps every matching request waiting until release() is called, as a slow network would.
async function hold(page: Page, matches: (url: URL) => boolean): Promise<() => void> {
  const waiting: Route[] = [];
  let released = false;
  await page.route((url) => matches(url), (route) => { if (released) void route.continue(); else waiting.push(route); });
  return () => { released = true; for (const route of waiting.splice(0)) void route.continue(); };
}

const box = async (locator: Locator) => {
  const found = await locator.boundingBox();
  expect(found, 'the element is on screen').not.toBeNull();
  return found!;
};

for (const viewport of [{ width: 390, height: 844 }, { width: 1280, height: 800 }]) {
  test.describe(`at ${viewport.width} px`, () => {
    test.use({ viewport });

    test('the list and the timeline hold their shape while they load, and nothing moves when the data arrives', async ({ page }) => {
      const suffix = `${Date.now()}-${process.pid}`;
      await redeemLink(page, createWorkspaceWithSlug(`E2E skeletons ${suffix}`, `owner-${suffix}@example.com`).url);
      const [id] = await conversations(page, 3);

      const releaseList = await hold(page, (url) => url.pathname === '/api/v1/conversations');
      await page.goto(`${base}/`);
      const status = page.locator('.conversation-scroll').getByRole('status');
      await expect(status).toHaveText('Loading workspace…');
      await expect(status).toHaveClass('sr-only');
      const blockRow = page.locator('.conversation-scroll .loading-shape .conversation-row').first();
      const rowBefore = await box(blockRow);
      // Waiting longest, the default order, has no day labels; Latest activity starts with one.
      await expect(page.locator('.conversation-scroll .loading-shape .group-label')).toHaveCount(0);
      releaseList();
      const realRow = page.locator('.conversation-scroll > .conversation-item .conversation-row').first();
      await expect(page.locator('.conversation-scroll > .conversation-item')).toHaveCount(3);
      await expect(page.locator('.conversation-scroll .loading-shape')).toHaveCount(0);
      await expect(page.locator('.conversation-scroll > .group-label')).toHaveCount(0);
      const rowAfter = await box(realRow);
      expect(Math.abs(rowAfter.y - rowBefore.y), 'the first row starts where its block was').toBeLessThanOrEqual(1);
      expect(Math.abs(rowAfter.height - rowBefore.height), 'a row is as tall as its block').toBeLessThanOrEqual(1);

      await page.unrouteAll({ behavior: 'ignoreErrors' });
      const releaseLatest = await hold(page, (url) => url.pathname === '/api/v1/conversations');
      await page.goto(`${base}/?sort=updated_desc`);
      const blockGroup = page.locator('.conversation-scroll .loading-shape .group-label');
      const [latestRowBefore, groupBefore] = [await box(blockRow), await box(blockGroup)];
      releaseLatest();
      await expect(page.locator('.conversation-scroll > .conversation-item')).toHaveCount(3);
      const [latestRowAfter, groupAfter] = [await box(realRow), await box(page.locator('.conversation-scroll > .group-label').first())];
      expect(Math.abs(groupAfter.y - groupBefore.y), 'the day label stays where its block was').toBeLessThanOrEqual(1);
      expect(Math.abs(groupAfter.height - groupBefore.height), 'the day label is as tall as its block').toBeLessThanOrEqual(1);
      expect(Math.abs(latestRowAfter.y - latestRowBefore.y), 'the first row starts where its block was').toBeLessThanOrEqual(1);
      await page.unrouteAll({ behavior: 'ignoreErrors' });

      const releaseTimeline = await hold(page, (url) => url.pathname === `/api/v1/conversations/${id}/messages`);
      await page.goto(`${base}/conversations/${id}`);
      const blockMessage = page.locator('.timeline-stack .loading-shape .message').first();
      await expect(blockMessage).toBeVisible();
      const [messageBefore, metaBefore] = [await box(blockMessage), await box(blockMessage.locator('.message-meta'))];
      await page.screenshot({ path: test.info().outputPath(`timeline-loading-${viewport.width}.png`) });
      releaseTimeline();
      const message = page.locator('article.message').first();
      await expect(message).toBeVisible();
      await expect(page.locator('.timeline-stack .loading-shape')).toHaveCount(0);
      const [messageAfter, metaAfter] = [await box(message), await box(message.locator('.message-meta'))];
      expect(Math.abs(messageAfter.y - messageBefore.y), 'the first message starts where its block was').toBeLessThanOrEqual(1);
      expect(Math.abs(messageAfter.x - messageBefore.x), 'the first message is as wide as its block').toBeLessThanOrEqual(1);
      expect(Math.abs(messageAfter.width - messageBefore.width), 'the first message is as wide as its block').toBeLessThanOrEqual(1);
      expect(Math.abs(metaAfter.height - metaBefore.height), 'its author line is as tall as its block').toBeLessThanOrEqual(1);
    });
  });
}
