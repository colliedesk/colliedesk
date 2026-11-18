import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

async function inboxWithConversations(page: Page, count: number): Promise<void> {
  const suffix = `${Date.now()}-${process.pid}`;
  await redeemLink(page, createWorkspaceWithSlug(`Tooltip Desk ${suffix}`, `owner-${suffix}@example.com`).url);
  const headers = { Origin: servers.main };
  for (let index = 0; index < count; index++) {
    const contact = await page.request.post(`${servers.main}/api/v1/contacts`, { headers, data: { name: `Customer ${index}`, email: `tabs-${index}-${suffix}@example.com` } });
    expect(contact.status()).toBe(201);
    const { id } = await contact.json() as { id: string };
    const created = await page.request.post(`${servers.main}/api/v1/conversations`, { headers, data: { contact_id: id, subject: `Question ${index}` } });
    expect(created.status()).toBe(201);
  }
}

// A row's tooltip never hides behind the top edge of the list, which clips what leaves it (Paper "15 Inbox", I1-d).
for (const width of [1440, 1280]) {
  test(`at ${width} px the top row's tooltip stays inside the list`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await inboxWithConversations(page, 3);
    await page.goto(`${servers.main}/`);
    await expect(page.locator('.conversation-scroll > .conversation-item')).toHaveCount(3);

    // Hovering a row only shows its select box (I1-d); the full time shows on its own time, inside that row, so no
    // tooltip covers another row or hides behind the top edge of the list.
    const first = page.locator('.conversation-scroll > .conversation-item').first();
    await first.locator('.conversation-row').hover({ position: { x: 40, y: 40 } });
    expect.soft(await first.locator('.conversation-row').evaluate((button) => getComputedStyle(button, '::after').content), 'the row has no tooltip').toBe('none');
    await first.locator('.conversation-row time').hover();
    await page.waitForTimeout(300);
    const placement = await first.evaluate((item) => {
      const time = item.querySelector('.conversation-row time')!;
      const tip = getComputedStyle(time, '::after');
      const box = time.getBoundingClientRect();
      const row = item.getBoundingClientRect();
      const list = item.closest('.conversation-scroll')!.getBoundingClientRect();
      const top = box.top + parseFloat(tip.top);
      const left = box.left + parseFloat(tip.left);
      return { opacity: tip.opacity, top, bottom: top + parseFloat(tip.height), left, right: left + parseFloat(tip.width), row: { top: row.top, bottom: row.bottom }, list: { left: list.left, right: list.right } };
    });
    expect.soft(placement.opacity, 'the time shows its tooltip').toBe('1');
    expect.soft(placement.top, 'the tooltip starts inside its row').toBeGreaterThanOrEqual(placement.row.top);
    expect.soft(placement.bottom, 'the tooltip ends inside its row').toBeLessThanOrEqual(placement.row.bottom);
    expect.soft(placement.left, 'the tooltip starts inside the list').toBeGreaterThanOrEqual(placement.list.left);
    expect.soft(placement.right, 'the tooltip ends inside the list').toBeLessThanOrEqual(placement.list.right);
    await page.locator('.list-pane').screenshot({ path: test.info().outputPath(`list-${width}.png`) });
  });
}
