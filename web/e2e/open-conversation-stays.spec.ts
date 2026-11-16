import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// The open conversation stays on screen while an action moves it out of the list: off page 1 of Waiting
// longest after a reply, out of Open once resolved, out of Unassigned once assigned, out of a tag's list once
// the tag is removed. Its header, timeline and reply box never leave the page for a round trip, and a
// half-written reply survives. A full page of conversations (more than 50) is what moves a replied one off it.
const baseURL = servers.mail;
const headers = { Origin: baseURL };

interface Desk { suffix: string; ids: Record<string, string> }

async function fullDesk(page: Page): Promise<Desk> {
  const suffix = uniqueSuffix();
  const support = `support-${suffix}@acme.example`;
  await redeemLink(page, createWorkspace(`Stays ${suffix}`, `admin-${suffix}@acme.example`, baseURL));
  expect((await page.request.put(`${baseURL}/api/v1/channels/email`, { headers, data: { support_address: support } })).status()).toBe(200);
  const secret = (await (await page.request.post(`${baseURL}/api/v1/channels/email/secret`, { headers })).json() as { inbound_secret: string }).inbound_secret;
  const ids: Record<string, string> = {};
  // One after another, so Ticket 00 waited longest and heads the list.
  for (let n = 0; n < 52; n += 1) {
    const subject = `Ticket ${String(n).padStart(2, '0')} ${suffix}`;
    const response = await page.request.post(`${baseURL}/api/v1/channels/email/inbound`, {
      headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret },
      data: Buffer.from([`From: Customer ${n} <c${n}-${suffix}@customer.example>`, `To: ${support}`, `Subject: ${subject}`,
        `Message-ID: <m${n}-${suffix}@customer.example>`, 'MIME-Version: 1.0', 'Content-Type: text/plain; charset=utf-8', '', `Question ${n}`].join('\r\n')),
    });
    expect(response.status()).toBe(200);
    ids[String(n).padStart(2, '0')] = (await response.json() as { conversation_id: string }).conversation_id;
  }
  return { suffix, ids };
}

const subjectOf = (desk: Desk, number: string) => `Ticket ${number} ${desk.suffix}`;
const list = (page: Page) => page.getByRole('region', { name: 'Conversation list' });
const row = (page: Page, subject: string) => list(page).getByRole('button', { name: new RegExp(subject) });
const composer = (page: Page) => page.getByLabel('Message body').last();
const details = (page: Page) => page.getByRole('complementary', { name: 'Conversation details' });

// Opens the list at search, then the conversation's row, and starts watching the pane: every DOM change checks
// that the conversation's heading, its timeline and the reply box are on the page.
async function open(page: Page, search: string, subject: string): Promise<void> {
  await page.goto(`${baseURL}/${search}`);
  await row(page, subject).click();
  await expect(page.getByRole('region', { name: 'Timeline' }).getByRole('heading', { name: subject })).toBeVisible();
  await expect(composer(page)).toBeEnabled();
  await page.evaluate((title) => {
    const missing: string[] = [];
    (window as unknown as { missing: string[] }).missing = missing;
    const check = () => {
      const timeline = document.querySelector('section.timeline-panel');
      if (!timeline || !timeline.textContent?.includes(title)) missing.push('conversation');
      if (!document.querySelector('textarea[aria-label="Message body"]')) missing.push('reply box');
    };
    new MutationObserver(check).observe(document.body, { subtree: true, childList: true, characterData: true });
  }, subject);
}

// The action took the conversation out of the list, and the pane never lost it meanwhile.
async function expectStayed(page: Page, subject: string): Promise<void> {
  await expect(row(page, subject)).toHaveCount(0);
  // The by-id read answers after the list: give it time to come back before reading what was missed.
  await expect(page.getByRole('region', { name: 'Timeline' }).getByRole('heading', { name: subject })).toBeVisible();
  await page.waitForTimeout(1_000);
  const missing = await page.evaluate(() => [...new Set((window as unknown as { missing: string[] }).missing)]);
  expect(missing, `${subject}: what left the screen while it left the list`).toEqual([]);
}

test('the open conversation stays on screen with its reply box while an action moves it out of the list', async ({ page }) => {
  test.setTimeout(120_000);
  await page.setViewportSize({ width: 1440, height: 900 });
  const desk = await fullDesk(page);

  // Send reply: the customer no longer wrote last, so Waiting longest moves it behind 51 waiting ones, off page 1.
  await open(page, '', subjectOf(desk, '00'));
  await composer(page).fill(`Answered (${desk.suffix})`);
  // Nothing seeds a replied conversation: its by-id read answers after the list, and the pane is watched until then.
  const read = page.waitForResponse((response) => response.request().method() === 'GET' && new URL(response.url()).pathname === `/api/v1/conversations/${desk.ids['00']}`);
  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(page.getByRole('region', { name: 'Timeline' }).getByText(`Answered (${desk.suffix})`)).toBeVisible();
  await read;
  await expectStayed(page, subjectOf(desk, '00'));

  // Send and resolve: it leaves Open.
  await open(page, '', subjectOf(desk, '01'));
  await composer(page).fill(`Answered and resolved (${desk.suffix})`);
  await page.getByRole('button', { name: 'Send and resolve' }).click();
  await expect(page.getByRole('region', { name: 'Timeline' }).getByText(`Answered and resolved (${desk.suffix})`)).toBeVisible();
  await expectStayed(page, subjectOf(desk, '01'));

  // Resolve with a reply half written: it leaves Open and the draft stays in the box.
  await open(page, '', subjectOf(desk, '02'));
  await composer(page).fill('Half-written answer');
  await page.getByRole('button', { name: 'Resolve', exact: true }).click();
  await expectStayed(page, subjectOf(desk, '02'));
  await expect(composer(page)).toHaveValue('Half-written answer');

  // Assign in the Unassigned list: it leaves the list.
  const me = await (await page.request.get(`${baseURL}/api/v1/auth/me`)).json() as { agent: { id: string } };
  await open(page, '?assignee=unassigned', subjectOf(desk, '03'));
  await composer(page).fill('Half-written answer');
  await details(page).getByLabel('Assignee').selectOption(me.agent.id);
  await expectStayed(page, subjectOf(desk, '03'));
  await expect(composer(page)).toHaveValue('Half-written answer');

  // Remove the tag in that tag's list: it leaves the list.
  const tag = await (await page.request.post(`${baseURL}/api/v1/tags`, { headers, data: { name: `billing ${desk.suffix}` } })).json() as { id: string; name: string };
  for (const number of ['04', '05']) expect((await page.request.put(`${baseURL}/api/v1/conversations/${desk.ids[number]}/tags/${tag.id}`, { headers })).ok()).toBe(true);
  await open(page, `?tag=${tag.id}`, subjectOf(desk, '04'));
  await composer(page).fill('Half-written answer');
  await details(page).locator('.tags-pick summary').click();
  await details(page).getByRole('checkbox', { name: tag.name }).click();
  await expectStayed(page, subjectOf(desk, '04'));
  await expect(composer(page)).toHaveValue('Half-written answer');
});
