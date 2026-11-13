import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

const baseURL = servers.main;

async function signedIn(page: import('@playwright/test').Page) {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`E2E AI rows ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${baseURL}/`);
}

test('ai features: each row reads in at most two lines on a wide screen, and the sheet opens beside the list', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await signedIn(page);
  await page.goto(`${baseURL}/settings/ai`);
  const rows = page.getByRole('list', { name: 'Features' }).getByRole('listitem');
  await expect(rows).toHaveCount(3);
  for (const line of await page.locator('.ai-row-line').all()) {
    const lines = await line.evaluate((element) => element.getBoundingClientRect().height / Number.parseFloat(getComputedStyle(element).lineHeight));
    expect(lines, await line.innerText()).toBeLessThanOrEqual(2);
  }
  expect(await page.locator('.ai-state').first().evaluate((element) => element.getBoundingClientRect().height)).toBeLessThan(24);
  await page.getByRole('button', { name: 'Reply drafts settings' }).click();
  const sheet = page.getByRole('dialog', { name: 'Reply drafts' });
  await expect(sheet).toBeVisible();
  const box = (await sheet.boundingBox())!;
  expect(box.x + box.width, 'the sheet is on the right edge').toBeGreaterThan(1400);
  expect(box.width).toBeLessThan(480);
  await page.keyboard.press('Escape');
  await expect(sheet).toBeHidden();
});

test('ai features: on a phone every tab is reachable and a strip that scrolls shows it', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await signedIn(page);
  await page.goto(`${baseURL}/settings/ai`);
  const strip = page.getByRole('tablist', { name: 'Built-in AI settings' });
  await expect(strip).toBeVisible();
  const overflows = await strip.evaluate((element) => element.scrollWidth > element.clientWidth + 1);
  if (overflows) {
    await expect(strip).toHaveClass(/fade-end/);
    // The first tab the strip cuts shows more than its faded edge: part of its name is in plain sight, so the strip reads as scrolling.
    const room = await strip.evaluate((element) => {
      const edge = element.getBoundingClientRect().right;
      const cut = Array.from(element.querySelectorAll('[role="tab"]')).find((tab) => tab.getBoundingClientRect().right > edge);
      return cut ? edge - cut.getBoundingClientRect().left : 0;
    });
    expect(room, 'pixels of the first cut tab inside the strip').toBeGreaterThanOrEqual(20);
  }
  for (const name of ['Features', 'Providers', 'AI chat', 'Usage', 'History']) {
    const tab = strip.getByRole('tab', { name: new RegExp(`^${name}`) });
    await tab.scrollIntoViewIfNeeded();
    await tab.click();
    await expect(tab).toHaveAttribute('aria-selected', 'true');
    const at = (await tab.boundingBox())!;
    expect(at.x, `${name} is inside the screen`).toBeGreaterThanOrEqual(0);
    expect(at.x + at.width, `${name} is inside the screen`).toBeLessThanOrEqual(390);
  }
  await strip.getByRole('tab', { name: 'Features' }).click();
  await page.getByRole('button', { name: 'Triage suggestions settings' }).click();
  const sheet = page.getByRole('dialog', { name: 'Triage suggestions' });
  const sheetBox = (await sheet.boundingBox())!;
  expect(sheetBox.width, 'the sheet fills a phone').toBeGreaterThanOrEqual(388);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
