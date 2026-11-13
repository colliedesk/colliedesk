import { expect, test } from '@playwright/test';
import type { Browser, Page } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// The reply box in a real browser and real layout: a tab works on the first press.
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
