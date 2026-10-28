import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { servers } from './support/cli';
import { expectIdentityVisible, redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// A phone and a tablet, emulated with touch, so the app sizes its controls for a finger (`pointer: coarse`).
const phone = { viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true };
const tablet = { viewport: { width: 768, height: 1024 }, hasTouch: true, isMobile: true };

interface Desk { name: string; email: string }

// signInWithTickets makes a workspace with enough conversations to scroll a phone screen, newest last.
async function signInWithTickets(page: Page, label: string, count: number): Promise<Desk> {
  const suffix = `${Date.now()}-${process.pid}`;
  const desk = { name: `${label} ${suffix}`, email: `owner-${suffix}@example.com` };
  await redeemLink(page, createWorkspaceWithSlug(desk.name, desk.email).url);
  const headers = { Origin: servers.main };
  const contact = await page.request.post(`${servers.main}/api/v1/contacts`, { headers, data: { name: 'Phone Customer', email: `customer-${suffix}@example.com` } });
  expect(contact.status()).toBe(201);
  const { id } = await contact.json() as { id: string };
  for (let number = 1; number <= count; number += 1) {
    const created = await page.request.post(`${servers.main}/api/v1/conversations`, {
      headers, data: { contact_id: id, subject: `Phone ticket ${String(number).padStart(2, '0')}` },
    });
    expect(created.status()).toBe(201);
  }
  return desk;
}

// expectOnScreen proves a person sees the element without scrolling: inside the viewport and not
// covered by anything else (a sticky bar, the composer).
async function expectOnScreen(locator: Locator, what: string): Promise<void> {
  await expect(locator).toBeVisible();
  const problem = await locator.evaluate((element) => {
    const box = element.getBoundingClientRect();
    const where = `at ${Math.round(box.left)},${Math.round(box.top)}–${Math.round(box.right)},${Math.round(box.bottom)} in ${window.innerWidth}×${window.innerHeight}`;
    if (box.top < 0 || box.bottom > window.innerHeight || box.left < 0 || box.right > window.innerWidth) return `off screen ${where}`;
    const hit = document.elementFromPoint(box.left + box.width / 2, box.top + Math.min(box.height / 2, 10));
    if (hit === null || !(element === hit || element.contains(hit) || hit.contains(element))) {
      const cover = hit?.getBoundingClientRect();
      return `covered by ${hit?.outerHTML.slice(0, 60)} (${Math.round(cover?.top ?? 0)}–${Math.round(cover?.bottom ?? 0)}) ${where}, scrolled ${window.scrollY}`;
    }
    return '';
  });
  expect(problem, `${what} is on screen and not covered`).toBe('');
}

// expectTouchSized fails on a button smaller than a fingertip (44 px) or cut off by its box.
async function expectTouchSized(scope: Locator, what: string): Promise<void> {
  const problems = await scope.evaluate((root) => {
    const cardRight = root.getBoundingClientRect().right + 0.5;
    return Array.from(root.querySelectorAll('button')).flatMap((button) => {
      const box = button.getBoundingClientRect();
      if (box.width === 0 && box.height === 0) return [];
      const name = button.getAttribute('aria-label') ?? button.textContent?.trim() ?? '?';
      const issues: string[] = [];
      if (box.height < 44) issues.push(`${name}: ${box.height}px tall`);
      if (box.right > cardRight || box.right > window.innerWidth) issues.push(`${name}: cut off at the right`);
      return issues;
    });
  });
  expect(problems, `${what} buttons are at least 44 px and fully on screen`).toEqual([]);
}

async function expectNoOverflow(locator: Locator, what: string): Promise<void> {
  const overflow = await locator.evaluate((element) => element.scrollWidth - element.clientWidth);
  expect(overflow, `${what} does not overflow sideways`).toBeLessThanOrEqual(0);
}

test.describe('on a phone', () => {
  test.use(phone);

  test('an opened conversation takes the screen, and Back or the browser back returns to the list where it was', async ({ page }) => {
    const desk = await signInWithTickets(page, 'Phone Desk', 30);
    // A filtered list, so coming back also proves the filters stayed.
    const listURL = `${servers.main}/?status=all`;
    await page.goto(listURL);
    const list = page.getByRole('region', { name: 'Conversation list' });
    const row = list.getByRole('button', { name: /Phone ticket 05/ });
    await row.scrollIntoViewIfNeeded();
    await expectOnScreen(row, 'the row far down the list');
    await expectIdentityVisible(page, desk.name, desk.email);
    const listY = (await row.boundingBox())!.y;

    for (const goBack of [
      () => page.getByRole('button', { name: 'Back to Inbox' }).tap(),
      async () => { await page.goBack(); },
    ]) {
      await row.tap();
      await expect(page).toHaveURL(/\/conversations\/[0-9a-f-]+\?status=all$/);
      const thread = page.getByRole('region', { name: 'Timeline' });
      // The conversation replaces the list instead of sitting under it.
      await expect(list).toBeHidden();
      await expectOnScreen(thread.getByRole('heading', { name: 'Phone ticket 05' }), 'the conversation title');
      await expectOnScreen(page.getByRole('button', { name: 'Back to Inbox' }), 'Back to Inbox');
      await expectOnScreen(page.getByLabel('Message body').first(), 'the reply box');
      await expectOnScreen(page.getByRole('button', { name: 'Send reply' }), 'Send reply');
      await expectIdentityVisible(page, desk.name, desk.email);

      await goBack();
      await expect(page).toHaveURL(listURL);
      await expect(page.getByRole('heading', { name: 'Phone ticket 05' })).toBeHidden();
      await expectOnScreen(row, 'the row the agent opened');
      expect(Math.abs((await row.boundingBox())!.y - listY), 'the list is where the agent left it').toBeLessThanOrEqual(2);
    }
  });

  test('the thread header and the reply box fit a phone, with every button big enough for a finger', async ({ page }) => {
    await signInWithTickets(page, 'Phone Composer', 1);
    await page.goto(`${servers.main}/`);
    await page.getByRole('button', { name: /Phone ticket 01/ }).tap();
    const thread = page.getByRole('region', { name: 'Timeline' });
    await expect(thread.getByRole('heading', { name: 'Phone ticket 01' })).toBeVisible();
    await expectTouchSized(page.locator('.timeline-header'), 'the thread header');
    await expectTouchSized(page.locator('.composer-card'), 'the reply box');
    await expectNoOverflow(page.locator('.composer-actions'), 'the reply box toolbar');
    await expectNoOverflow(page.locator('html'), 'the page');
  });
});

test.describe('on a tablet', () => {
  test.use(tablet);

  test('the list stays beside the thread, and the reply toolbar fits with buttons big enough for a finger', async ({ page }) => {
    await signInWithTickets(page, 'Tablet Desk', 3);
    await page.goto(`${servers.main}/`);
    await page.getByRole('button', { name: /Phone ticket 02/ }).tap();
    await expect(page.getByRole('region', { name: 'Timeline' }).getByRole('heading', { name: 'Phone ticket 02' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Conversation list' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Back to Inbox' })).toBeHidden();
    await expectTouchSized(page.locator('.timeline-header'), 'the thread header');
    await expectTouchSized(page.locator('.composer-card'), 'the reply box');
    await expectNoOverflow(page.locator('.composer-actions'), 'the reply box toolbar');
    await expectOnScreen(page.getByRole('button', { name: 'Send and resolve' }), 'Send and resolve');
  });
});
