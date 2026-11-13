import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// Pages outside the inbox, drawn by a real browser: Knowledge base → Settings fits a phone, a settings page says
// its title once, and the sidebar's resize handle shows where the keyboard is.

async function newDesk(page: Page, label: string): Promise<void> {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspaceWithSlug(`${label} ${suffix}`, `owner-${suffix}@example.com`).url);
}

// visibleHeadings lists the h1 to h3 a sighted person sees in scope; a heading kept for screen readers only
// (one pixel, clipped) is not on screen.
async function visibleHeadings(scope: Locator): Promise<string[]> {
  return scope.evaluate((root) => Array.from(root.querySelectorAll<HTMLElement>('h1, h2, h3')).flatMap((heading) => {
    const box = heading.getBoundingClientRect();
    return box.width > 1 && box.height > 1 && getComputedStyle(heading).visibility !== 'hidden' ? [heading.textContent?.trim() ?? ''] : [];
  }));
}

test('on a 390 px screen Knowledge base → Settings fits the screen, with nothing to scroll sideways', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await newDesk(page, 'Narrow Help Center');
  await page.goto(`${servers.main}/knowledge/settings`);
  const card = page.getByRole('region', { name: 'Help center' });
  await expect(card.locator('.settings-body input').first()).toBeVisible();
  const widths = await page.evaluate(() => {
    const main = document.querySelector('main')!;
    return { page: document.documentElement.scrollWidth, main: main.scrollWidth, mainBox: main.clientWidth, screen: window.innerWidth };
  });
  expect(widths.page, 'the page is no wider than the screen').toBeLessThanOrEqual(widths.screen);
  expect(widths.main, 'the knowledge base frame does not scroll sideways').toBeLessThanOrEqual(widths.mainBox);
  const right = await card.evaluate((section) => Math.max(...Array.from(section.querySelectorAll<HTMLElement>('h2, label, input, select, textarea, button'))
    .map((element) => element.getBoundingClientRect().right)));
  expect(right, 'every field of the help center card ends on screen').toBeLessThanOrEqual(390);
  await page.screenshot({ path: test.info().outputPath('kb-settings-390.png'), fullPage: true });
});

test('General and Saved replies say their title once, and General carries no note about where the help center went', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDesk(page, 'Plain Settings');
  const headers = { Origin: servers.main };
  expect((await page.request.post(`${servers.main}/api/v1/saved-replies`, { headers, data: { title: 'Refund issued', body: 'We have issued the refund.' } })).status()).toBe(201);

  await page.goto(`${servers.main}/settings/general`);
  const general = page.locator('main.settings-page');
  await expect(general.getByRole('textbox', { name: 'Workspace name' })).toBeVisible();
  const generalHeadings = await visibleHeadings(general);
  expect(generalHeadings.filter((text) => text === 'General'), 'General is said once').toHaveLength(1);
  await expect(general.getByText('Now in Knowledge.')).toHaveCount(0);
  await expect(general.getByRole('link', { name: 'Open Help center settings' })).toHaveCount(0);
  // The anchor old links use still lands on the section.
  await expect(general.locator('#general-heading')).toHaveCount(1);
  await page.screenshot({ path: test.info().outputPath('settings-general-1440.png') });

  await page.goto(`${servers.main}/settings/saved-replies`);
  const replies = page.locator('main.settings-page');
  await expect(replies.getByText('Refund issued')).toBeVisible();
  const replyHeadings = await visibleHeadings(replies);
  expect(replyHeadings.filter((text) => text === 'Saved replies'), 'Saved replies is said once').toHaveLength(1);
  const lead = 'Text your whole team inserts into a reply.';
  const leads = await replies.evaluate((root, lead) => Array.from(root.querySelectorAll<HTMLElement>('p'))
    .filter((p) => p.getBoundingClientRect().height > 1 && (p.textContent ?? '').includes(lead)).length, lead);
  expect(leads, 'the line under the title is said once').toBe(1);
  await expect(replies.locator('#saved-replies-heading')).toHaveCount(1);
  await page.screenshot({ path: test.info().outputPath('settings-saved-replies-1440.png') });
});

test('the sidebar resize handle shows the focus ring other controls show when the keyboard reaches it', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDesk(page, 'Focus Desk');
  await page.goto(`${servers.main}/settings/account`);
  const handle = page.getByRole('separator', { name: 'Resize sidebar' });
  await expect(handle).toBeAttached();
  for (let step = 0; step < 60 && !(await handle.evaluate((element) => element === document.activeElement)); step += 1) await page.keyboard.press('Tab');
  await expect(handle).toBeFocused();
  const ring = await handle.evaluate((element) => {
    const probe = document.createElement('span');
    probe.style.color = 'var(--focus)';
    document.body.append(probe);
    const focus = getComputedStyle(probe).color;
    probe.remove();
    const style = getComputedStyle(element);
    return { style: style.outlineStyle, width: parseFloat(style.outlineWidth), color: style.outlineColor, focus };
  });
  expect(ring.style, 'an outline is drawn').not.toBe('none');
  expect(ring.width, 'as wide as other controls draw it').toBeGreaterThanOrEqual(2);
  expect(ring.color, 'in the focus colour').toBe(ring.focus);
  await page.screenshot({ path: test.info().outputPath('sidebar-resizer-focus-1440.png'), clip: { x: 0, y: 0, width: 600, height: 400 } });
});
