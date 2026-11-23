import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

// The Filters menu of the knowledge base list reads as groups: each group's title runs the full width above its
// fields, and a date range keeps From and To side by side on one row, on a laptop and on a phone.
test('knowledge base Filters: a date group\'s title spans the menu and From and To share a row (1440 px)', { tag: '@journey' }, async ({ page }) => {
  await expectFiltersLaidOut(page, { width: 1440, height: 900 });
});

test('knowledge base Filters: a date group\'s title spans the menu and From and To share a row (390 px)', { tag: '@journey' }, async ({ page }) => {
  await expectFiltersLaidOut(page, { width: 390, height: 844 });
});

async function expectFiltersLaidOut(page: Page, viewport: { width: number; height: number }) {
  await page.setViewportSize(viewport);
  const suffix = `${Date.now()}-${process.pid}-${viewport.width}`;
  const { url } = createWorkspaceWithSlug(`KB Filters ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  await page.goto(`${servers.main}/knowledge`);
  await page.locator('summary').filter({ hasText: /^Filters/ }).click();
  const menu = page.getByRole('group', { name: 'Filters' });
  await expect(menu).toBeVisible();

  for (const name of ['Created', 'Updated']) await expectDateGroupLaidOut(page, name);

  const box = (await menu.boundingBox())!;
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(viewport.width + 0.5);
}

async function expectDateGroupLaidOut(page: Page, name: string) {
  const group = page.getByRole('group', { name, exact: true });
  const legend = group.locator('legend');
  const from = group.getByLabel(`${name} from`);
  const to = group.getByLabel(`${name} to`);
  const [groupBox, legendBox, fromBox, toBox] = await Promise.all([group, legend, from, to].map(async (item) => (await item.boundingBox())!));
  // The title is a full-width row of its own, above both dates.
  expect(legendBox.width).toBeGreaterThanOrEqual(groupBox.width - 1);
  expect(legendBox.y + legendBox.height).toBeLessThanOrEqual(Math.min(fromBox.y, toBox.y) + 0.5);
  // From and To sit on one row, From first.
  expect(Math.abs(fromBox.y - toBox.y)).toBeLessThan(1);
  expect(fromBox.x + fromBox.width).toBeLessThanOrEqual(toBox.x + 0.5);
  // Neither date is squeezed below what a date field needs to show dd/mm/yyyy and its picker button.
  for (const field of [fromBox, toBox]) expect(field.width).toBeGreaterThanOrEqual(120);
}
