import { expect, test } from '@playwright/test';
import type { Browser, Locator, Page } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// The reply box in a real browser and real layout: a tab works on the first press, and the @ list of a private note
// opens at the @ itself, as in Slack, Linear and GitHub.
const baseURL = servers.main;
const headers = { Origin: baseURL };

// A person's press: the button goes down, a moment passes, it comes up. A box that moved meanwhile loses the click.
const press = { delay: 150 };

// openConversation signs in the writer of a new workspace, with the given teammates joined, on a new conversation.
async function openConversation(browser: Browser, page: Page, teammates: string[] = []): Promise<void> {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Composer Desk ${suffix}`, `tu-${suffix}@example.com`));
  for (const name of teammates) {
    const invite = await page.request.post(`${baseURL}/api/v1/auth/invites`, { headers, data: { email: `${name.split(' ')[0].toLowerCase()}-${suffix}@example.com`, roles: ['Agent'] } });
    expect(invite.status()).toBe(201);
    const context = await browser.newContext();
    const teammate = await context.newPage();
    await redeemLink(teammate, (await invite.json() as { url: string }).url);
    expect((await teammate.request.patch(`${baseURL}/api/v1/agents/me/display-name`, { headers, data: { display_name: name } })).status()).toBe(200);
    await context.close();
  }
  const contact = await (await page.request.post(`${baseURL}/api/v1/contacts`, { headers, data: { name: 'Hana Sato', email: `hana-${suffix}@example.com` } })).json() as { id: string };
  const conversation = await (await page.request.post(`${baseURL}/api/v1/conversations`, { headers, data: { contact_id: contact.id, subject: 'Refund still not on my card' } })).json() as { id: string };
  await page.goto(`${baseURL}/conversations/${conversation.id}`);
  await expect(page.getByRole('region', { name: 'Timeline' }).getByRole('heading', { name: 'Refund still not on my card' })).toBeVisible();
}

const composerFocused = (page: Page) => page.evaluate(() => Boolean(document.activeElement?.closest('.composer-card')));

test('one press on Private note or Reply switches the box while it is not focused', async ({ browser, page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await openConversation(browser, page);
  const tabs = page.getByRole('group', { name: 'Message type' });
  const note = tabs.getByRole('button', { name: /^Private note/ });
  const reply = tabs.getByRole('button', { name: /^Reply/ });
  await expect(reply).toHaveAttribute('aria-pressed', 'true');
  expect(await composerFocused(page), 'the box starts without the focus').toBe(false);

  await note.click(press);
  await expect(note, 'the first press on Private note switches to a note').toHaveAttribute('aria-pressed', 'true');

  // The focus leaves the box (a press on the conversation's title), then Reply gets one press.
  await page.getByRole('region', { name: 'Timeline' }).getByRole('heading', { name: 'Refund still not on my card' }).click();
  expect(await composerFocused(page), 'the box lost the focus').toBe(false);
  await reply.click(press);
  await expect(reply, 'the first press on Reply switches back to a reply').toHaveAttribute('aria-pressed', 'true');
});

interface Line { top: number; bottom: number; at: number }

// lineOf says where the browser draws line `index` (from 0) of the box's text and where on it `before` ends, from the
// box's own box, padding, scroll and font (a canvas measures the words), never from the composer's code.
function lineOf(box: Locator, index: number, before: string): Promise<Line> {
  return box.evaluate((element, [index, before]) => {
    const area = element as HTMLTextAreaElement;
    const style = getComputedStyle(area);
    const rect = area.getBoundingClientRect();
    const height = parseFloat(style.lineHeight);
    const top = rect.top + area.clientTop + parseFloat(style.paddingTop) - area.scrollTop + index * height;
    const context = document.createElement('canvas').getContext('2d')!;
    context.font = style.font;
    const at = rect.left + area.clientLeft + parseFloat(style.paddingLeft) + context.measureText(before).width;
    return { top, bottom: top + height, at };
  }, [index, before] as const);
}

// popoverOf is the box people see around the list: the list and its label (Told in Mentions and by email).
const popoverOf = (list: Locator) => list.locator('xpath=..');

async function expectInsideViewport(page: Page, box: { x: number; y: number; width: number; height: number }): Promise<void> {
  const viewport = page.viewportSize()!;
  expect(box.x, 'the list starts inside the screen').toBeGreaterThanOrEqual(0);
  expect(box.y, 'the list starts inside the screen').toBeGreaterThanOrEqual(0);
  expect(box.x + box.width, 'the list ends inside the screen').toBeLessThanOrEqual(viewport.width);
  expect(box.y + box.height, 'the list ends inside the screen').toBeLessThanOrEqual(viewport.height);
}

test('the @ list of a private note opens at the @: under its line, above it when there is no room, inside a phone screen', async ({ browser, page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await openConversation(browser, page, ['Long Bui', 'Loan Pham']);
  await page.getByRole('group', { name: 'Message type' }).getByRole('button', { name: /^Private note/ }).click();
  const box = page.getByLabel('Message body');
  await box.click();
  await box.pressSequentially('Can you check, @');
  const list = page.getByRole('listbox', { name: 'Told in Mentions and by email' });
  await expect(list.getByRole('option')).toHaveCount(2);

  // Under the line of the @, its left edge at the @. The box grows as it takes the focus (a short transition), so the
  // places are compared once both have settled.
  await expect(async () => {
    const line = await lineOf(box, 0, 'Can you check, ');
    const popover = (await popoverOf(list).boundingBox())!;
    expect(Math.abs(popover.x - line.at), `the list's left edge (${popover.x}) is at the @ (${line.at})`).toBeLessThanOrEqual(4);
    expect(popover.y, `the list (${popover.y}) starts under the line of the @ (${line.bottom})`).toBeGreaterThanOrEqual(line.bottom - 1);
    expect(popover.y - line.bottom, 'the list starts just under that line').toBeLessThanOrEqual(8);
    const listbox = (await list.boundingBox())!;
    expect(listbox.x - popover.x, 'the listbox sits in that box').toBeGreaterThanOrEqual(0);
    expect(listbox.x - popover.x, 'the listbox sits in that box').toBeLessThanOrEqual(8);
    expect(listbox.y, 'the listbox is under the line of the @').toBeGreaterThan(line.bottom);
    await expectInsideViewport(page, popover);
  }).toPass({ timeout: 3_000 });
  await page.screenshot({ path: testInfo.outputPath('mention-under-line.png') });
  // The keys still move in the list while it floats there.
  const second = (await list.getByRole('option').nth(1).locator('.mention-name').textContent())!;
  await box.press('ArrowDown');
  await expect(list.getByRole('option', { selected: true })).toContainText(second);
  await box.press('Escape');
  await expect(list).toBeHidden();

  // An @ on the last line of a full box has no room under it before the foot of the screen: the list opens above it.
  await box.fill('');
  const lines = 30;
  await box.pressSequentially('Notes'.concat('\n'.repeat(lines - 1)));
  await box.pressSequentially('@');
  await expect(list.getByRole('option')).toHaveCount(2);
  await expect(async () => {
    const last = await lineOf(box, lines - 1, '');
    const above = (await popoverOf(list).boundingBox())!;
    expect(above.y + above.height, `the list (bottom ${above.y + above.height}) ends above the line of the @ (${last.top})`).toBeLessThanOrEqual(last.top + 1);
    expect(last.top - (above.y + above.height), 'the list ends just above that line').toBeLessThanOrEqual(8);
    expect(Math.abs(above.x - last.at), `its left edge (${above.x}) is at the @ (${last.at})`).toBeLessThanOrEqual(4);
    await expectInsideViewport(page, above);
  }).toPass({ timeout: 3_000 });
  await page.screenshot({ path: testInfo.outputPath('mention-above-line.png') });
  await box.press('Escape');

  // On a phone an @ at the end of a long line keeps the whole list on the screen.
  await page.setViewportSize({ width: 390, height: 844 });
  await box.fill('');
  await box.click();
  await box.pressSequentially('Please ask the bank today @');
  await expect(list.getByRole('option')).toHaveCount(2);
  await expect(async () => expectInsideViewport(page, (await popoverOf(list).boundingBox())!)).toPass({ timeout: 3_000 });
  await page.screenshot({ path: testInfo.outputPath('mention-phone.png') });
  const first = (await list.getByRole('option').first().locator('.mention-name').textContent())!;
  await box.press('Enter');
  await expect(box).toHaveValue(`Please ask the bank today @${first} `);
});
