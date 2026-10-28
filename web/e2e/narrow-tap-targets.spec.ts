import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// A narrow window (a phone browser that does not report a touch pointer, or a small desktop window)
// still gets fingertip-sized controls: 44 px tall on any viewport 480 px wide or less.
test.use({ viewport: { width: 375, height: 812 } });

const controls = 'a, button, input:not([type="checkbox"]):not([type="radio"]):not([type="file"]), select, textarea';

// expectTapSized lists every visible control in scope shorter than 44 px.
async function expectTapSized(scope: Locator, what: string): Promise<void> {
  await expect(scope.first()).toBeVisible();
  const small = await scope.first().evaluate((root, selector) => Array.from(root.querySelectorAll<HTMLElement>(selector)).flatMap((element) => {
    const box = element.getBoundingClientRect();
    if (box.width === 0 || box.height === 0 || getComputedStyle(element).visibility === 'hidden') return [];
    const name = element.getAttribute('aria-label') ?? element.textContent?.trim().slice(0, 40) ?? element.tagName;
    return box.height < 44 ? [`${name}: ${Math.round(box.height)}px`] : [];
  }), controls);
  expect(small, `${what}: every control is at least 44 px tall`).toEqual([]);
}

async function newDesk(page: Page, label: string): Promise<void> {
  const suffix = `${Date.now()}-${process.pid}`;
  await redeemLink(page, createWorkspaceWithSlug(`${label} ${suffix}`, `owner-${suffix}@example.com`).url);
}

test('on a 375 px screen the empty inbox steps, the composer, the email channel form and the settings sections are tap-sized', async ({ page }) => {
  await newDesk(page, 'Narrow Desk');
  await page.goto(`${servers.main}/`);
  await expectTapSized(page.getByRole('region', { name: 'Get started' }).getByRole('list'), 'empty inbox next steps');

  const headers = { Origin: servers.main };
  const contact = await page.request.post(`${servers.main}/api/v1/contacts`, { headers, data: { name: 'Narrow Customer', email: `narrow-${Date.now()}@example.com` } });
  expect(contact.status()).toBe(201);
  const { id } = await contact.json() as { id: string };
  const created = await page.request.post(`${servers.main}/api/v1/conversations`, { headers, data: { contact_id: id, subject: 'Narrow question' } });
  expect(created.status()).toBe(201);
  const { id: conversation } = await created.json() as { id: string };
  await page.goto(`${servers.main}/conversations/${conversation}`);
  await expect(page.getByLabel('Message body')).toBeVisible();
  await expectTapSized(page.locator('.composer-head'), 'composer tabs');
  await expectTapSized(page.locator('.composer-actions'), 'composer actions');
  await page.getByRole('button', { name: /Insert saved reply/ }).click();
  await expectTapSized(page.locator('.picker-search'), 'saved replies search');
  await page.keyboard.press('Escape');

  await page.goto(`${servers.main}/settings/channels/type/email`);
  await expect(page.getByRole('textbox', { name: 'Support address' })).toBeVisible();
  await expectTapSized(page.locator('form').filter({ has: page.getByRole('textbox', { name: 'Support address' }) }), 'email channel form');
  await expectTapSized(page.getByRole('navigation', { name: 'Settings sections' }), 'settings sections');
});
