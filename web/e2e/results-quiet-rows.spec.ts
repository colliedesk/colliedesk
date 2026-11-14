import { expect, test, type Page } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// docs/internals/design.md › Quiet by default: on Reports a metric row shows its name, its value and one short
// line; how a number is counted opens from the tip beside its name. A new workspace has no import and almost no
// history, so every row says what it still needs instead of showing prose.
const limit = 60;

async function openReports(page: Page, suffix: string) {
  await redeemLink(page, createWorkspace(`Reports Quiet ${suffix}`, `owner-${suffix}@example.com`));
  await page.goto(`${servers.main}/results`);
  const rows = page.locator('.results-row');
  await expect(rows).toHaveCount(5);
  // The numbers are worked out in the background: the first look says so, then every row has its value.
  await expect(page.getByText('Calculating…')).toHaveCount(0, { timeout: 90_000 });
  return rows;
}

// shareLink is a link that opens the page of a workspace's numbers without signing in; the admin makes it through the API.
async function shareLink(page: Page) {
  const response = await page.request.post(`${servers.main}/api/v1/results/share-link`, { headers: { Origin: servers.main } });
  expect(response.status()).toBe(201);
  return (await response.json() as { url: string }).url;
}

// lines are the muted lines under the value of each metric row, as a person reads them.
const lines = (page: Page) => page.locator('.results-row .results-muted').evaluateAll((nodes) => nodes.filter((node) => (node as HTMLElement).offsetParent !== null).map((node) => node.textContent ?? ''));

test.describe('on a desktop screen', () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test('a new workspace gets short lines under each metric, and the way it counts opens from a tip', async ({ page }) => {
    const rows = await openReports(page, uniqueSuffix());
    await expect(page.getByText('How your team is doing in Colliedesk. Every number opens the conversations it counts.')).toBeVisible();
    await expect(page.getByText(/How your team did in Freshdesk/)).toHaveCount(0);
    // Nothing was imported: there is no comparison for the page to announce.
    await expect(page.getByText(/not compared yet/)).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'Nothing from Freshdesk to compare with yet' })).toBeVisible();

    const shown = await lines(page);
    expect(shown.length).toBeGreaterThan(0);
    for (const text of shown) expect(text.length, text).toBeLessThanOrEqual(limit);

    await expect(page.getByText('How we count')).toHaveCount(0);
    const tip = rows.nth(1).getByRole('button', { name: 'About Time to resolve' });
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await tip.click();
    await expect(page.getByRole('tooltip')).toContainText('counting only resolutions that stayed resolved for 7 days');
    await page.keyboard.press('Escape');
    await expect(page.getByRole('tooltip')).toHaveCount(0);

    // The full definitions stay one click away.
    await expect(page.getByRole('link', { name: 'How Reports counts, in full' })).toHaveAttribute('href', 'https://colliedesk.com/docs/results');
  });
});

test.describe('on a 390 px touch screen', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('rows stack without a sideways scroll, and a tip opens by tap and stays on screen', async ({ page }) => {
    const rows = await openReports(page, uniqueSuffix());
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'no sideways scroll').toBe(true);
    for (const text of await lines(page)) expect(text.length, text).toBeLessThanOrEqual(limit);

    // On a phone the column heads are hidden, so each card names the days it counts. A resolve row counts the window that
    // opens 7 days before the one customers' conversations were created in; the API says which days each is.
    const zone = (await page.getByText(/^Dates in your timezone, /).first().textContent())!.replace('Dates in your timezone, ', '');
    const windows = (await (await page.request.get(`${servers.main}/api/v1/results`)).json() as { current: { windows: { '30d': { created: { from: string; to: string }; resolved_created: { from: string; to: string } } } } }).current.windows['30d'];
    const days = (from: string, to: string) => {
      const format = (value: string) => new Date(value).toLocaleDateString('en', { month: 'short', day: 'numeric', timeZone: zone });
      return `Created ${format(from)} – ${format(to)}`;
    };
    const dayLabel = (row: number) => rows.nth(row).locator('.results-cell-label .results-narrow');
    await expect(dayLabel(0)).toHaveText(days(windows.created.from, windows.created.to));
    await expect(dayLabel(3)).toHaveText(days(windows.created.from, windows.created.to));
    for (const resolveRow of [1, 2, 4]) await expect(dayLabel(resolveRow)).toHaveText(days(windows.resolved_created.from, windows.resolved_created.to));
    expect(days(windows.resolved_created.from, windows.resolved_created.to)).not.toBe(days(windows.created.from, windows.created.to));

    const tip = rows.first().getByRole('button', { name: 'About First response time' });
    await tip.scrollIntoViewIfNeeded();
    await tip.tap();
    const layer = page.getByRole('tooltip');
    await expect(layer).toContainText('Time from a customer’s first message to the first reply');
    await page.waitForTimeout(300);
    await expect(layer).toBeVisible();
    const box = await layer.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(8);
    expect(box!.x + box!.width).toBeLessThanOrEqual(390 - 8);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'no sideways scroll with the tip open').toBe(true);
  });
});

test.describe('the page of a share link', () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test('shows the same quiet rows and tips to a visitor, and no control for the team', async ({ page, browser }) => {
    const suffix = uniqueSuffix();
    await redeemLink(page, createWorkspace(`Reports Shared ${suffix}`, `owner-${suffix}@example.com`));
    // The first numbers are worked out in the background; the link shows them once they exist.
    await page.goto(`${servers.main}/results`);
    await expect(page.locator('.results-row')).toHaveCount(5);
    await expect(page.getByText('Calculating…')).toHaveCount(0, { timeout: 90_000 });
    const url = await shareLink(page);

    const visitor = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const shared = await visitor.newPage();
    await shared.goto(url);
    await expect(shared.getByRole('heading', { level: 1, name: /: support results$/ })).toBeVisible();
    await expect(shared.locator('.results-row')).toHaveCount(5);
    for (const text of await lines(shared)) expect(text.length, text).toBeLessThanOrEqual(limit);
    await expect(shared.getByText('How we count')).toHaveCount(0);
    await expect(shared.getByText('Needs 5 answered conversations ·')).toBeVisible();
    const tip = shared.getByRole('button', { name: 'About Time to resolve' });
    await tip.click();
    await expect(shared.getByRole('tooltip')).toContainText('counting only resolutions that stayed resolved for 7 days');
    await shared.keyboard.press('Escape');
    // Nothing for the team on this page: no share or email controls, no way into the import or the conversations.
    await expect(shared.getByRole('heading', { name: 'Share and email' })).toHaveCount(0);
    await expect(shared.getByRole('button', { name: /Create (a new )?link|Turn off/ })).toHaveCount(0);
    await expect(shared.getByRole('switch', { name: 'Email me on Mondays' })).toHaveCount(0);
    await expect(shared.getByRole('link', { name: 'Open the import' })).toHaveCount(0);
    await expect(shared.locator('.results-row a[href*="result="]')).toHaveCount(0);
    await visitor.close();
  });
});
