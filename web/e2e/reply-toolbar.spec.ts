import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// The reply toolbar on a laptop or desktop: the tools on the left, both send buttons together on one row at the right.
async function openTicket(page: Page): Promise<void> {
  const suffix = `${Date.now()}-${process.pid}`;
  await redeemLink(page, createWorkspaceWithSlug(`Toolbar Desk ${suffix}`, `owner-${suffix}@example.com`).url);
  const headers = { Origin: servers.main };
  const contact = await page.request.post(`${servers.main}/api/v1/contacts`, { headers, data: { name: 'Toolbar Customer', email: `customer-${suffix}@example.com` } });
  expect(contact.status()).toBe(201);
  const { id } = await contact.json() as { id: string };
  expect((await page.request.post(`${servers.main}/api/v1/conversations`, { headers, data: { contact_id: id, subject: 'Toolbar ticket' } })).status()).toBe(201);
  await page.goto(`${servers.main}/`);
  await page.getByRole('button', { name: /Toolbar ticket/ }).click();
  await expect(page.getByRole('region', { name: 'Timeline' }).getByRole('heading', { name: 'Toolbar ticket' })).toBeVisible();
}

// The widths of common laptop and desktop screens: the reply box is narrower than its 46rem cap on the smaller ones.
for (const width of [1280, 1440, 1536, 1920]) {
  test(`at ${width} px Send reply stays on the row of Send and resolve`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await openTicket(page);
    const send = (await page.getByRole('button', { name: 'Send reply' }).boundingBox())!;
    const resolve = (await page.getByRole('button', { name: 'Send and resolve' }).boundingBox())!;
    expect(Math.abs(send.y - resolve.y), 'Send reply is on the row of Send and resolve').toBeLessThanOrEqual(1);
    // From a 1440 px screen Draft with AI keeps its name, also when it is off: an icon and "Off" alone say little.
    if (width >= 1440) await expect(page.locator('.composer-actions').getByText('Draft with AI')).toBeVisible();
    const overflow = await page.locator('.composer-actions').evaluate((bar) => bar.scrollWidth - bar.clientWidth);
    expect(overflow, 'the toolbar does not overflow sideways').toBeLessThanOrEqual(0);
  });
}
