import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { createWorkspace, databaseFor, psql, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// A long conversation opens at its newest message, and what the person just sent is in view. Real browser,
// real layout, mail through the inbound route: the positions are bounding boxes, not scroll offsets.
const baseURL = servers.mail;
const mailDatabase = databaseFor(baseURL);
const phone = { viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true };
const desktop = { viewport: { width: 1440, height: 900 } };

interface Desk { support: string; secret: string; suffix: string }
interface Thread { id: string; subject: string }

async function signIn(page: Page, label: string): Promise<Desk> {
  const suffix = uniqueSuffix();
  const support = `support-${suffix}@acme.example`;
  await redeemLink(page, createWorkspace(`${label} ${suffix}`, `admin-${suffix}@acme.example`, baseURL));
  const headers = { Origin: baseURL };
  expect((await page.request.put(`${baseURL}/api/v1/channels/email`, { headers, data: { support_address: support } })).status()).toBe(200);
  const secret = (await (await page.request.post(`${baseURL}/api/v1/channels/email/secret`, { headers })).json() as { inbound_secret: string }).inbound_secret;
  return { support, secret, suffix };
}

// A message long enough that nine of them are several screens tall.
const longBody = (label: string) => [label, ...Array.from({ length: 10 }, (_, index) =>
  `Paragraph ${index + 1} of ${label}: the parcel left the depot on Monday, the tracking page still says it is waiting for a driver, and nobody has called back about it.`)].join('\r\n\r\n');

// Mail threads onto a conversation only through a reply of ours, so the customer's earlier messages are rows
// the way the inbound route would have stored them: the conversation opens with the email as given (its newest
// message) and the older ones are written before it.
async function longThread(page: Page, desk: Desk, name: string, newest: string, html = false, earlier = 8): Promise<Thread> {
  const subject = `${name} ${desk.suffix}`;
  const customer = `${name.toLowerCase().replace(/\W+/g, '-')}-${desk.suffix}@customer.example`;
  const response = await page.request.post(`${baseURL}/api/v1/channels/email/inbound`, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': desk.secret },
    data: Buffer.from([`From: Nora Customer <${customer}>`, `To: ${desk.support}`, `Subject: ${subject}`, `Message-ID: <first-${desk.suffix}-${name.length}@customer.example>`,
      'MIME-Version: 1.0', `Content-Type: ${html ? 'text/html' : 'text/plain'}; charset=utf-8`, '', newest].join('\r\n')),
  });
  expect(response.status()).toBe(200);
  const created = await response.json() as { status: string; conversation_id: string };
  expect(created.status).toBe('created');
  for (let number = 1; number <= earlier; number += 1) {
    psql(`INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at)
      SELECT workspace_id,id,'incoming','${longBody(`Message ${number}`).replaceAll("'", "''")}',now()-interval '${earlier + 1 - number} hours' FROM conversations WHERE id='${created.conversation_id}'`, mailDatabase);
  }
  return { id: created.conversation_id, subject };
}

// arrive stores one more customer message now and touches the conversation, as the server does for mail that arrives.
function arrive(thread: Thread, body: string): void {
  psql(`INSERT INTO messages(workspace_id,conversation_id,kind,body)
    SELECT workspace_id,id,'incoming','${body.replaceAll("'", "''")}' FROM conversations WHERE id='${thread.id}'`, mailDatabase);
  psql(`UPDATE conversations SET updated_at=now() WHERE id='${thread.id}'`, mailDatabase);
}

const pane = (page: Page) => page.locator('.timeline-scroll');
const message = (page: Page, text: string) => pane(page).locator('article.message', { hasText: text });

// where says the message and the pane the person looks at, in page coordinates.
async function where(page: Page, target: Locator) {
  const box = await target.boundingBox();
  const area = await pane(page).boundingBox();
  const height = await page.evaluate(() => window.innerHeight);
  if (!box || !area) throw new Error('the message or the timeline is not drawn');
  return { top: box.y, bottom: box.y + box.height, paneTop: area.y, paneBottom: area.y + area.height, viewport: height };
}

// expectEndInView: the whole message is inside the timeline, which ends where the reply box starts.
async function expectEndInView(page: Page, target: Locator, what: string) {
  await expect.poll(async () => {
    const spot = await where(page, target);
    return spot.top >= spot.paneTop - 1 && spot.bottom <= spot.paneBottom + 1 && spot.bottom <= spot.viewport;
  }, { message: `${what} is fully inside the timeline`, timeout: 5_000 }).toBe(true);
}

async function expectStartInView(page: Page, target: Locator, what: string) {
  await expect.poll(async () => {
    const spot = await where(page, target);
    return spot.top >= spot.paneTop - 1 && spot.top < spot.paneBottom - 24;
  }, { message: `${what} starts inside the timeline`, timeout: 5_000 }).toBe(true);
}

const scrollOffset = (page: Page) => pane(page).evaluate((element) => element.scrollTop);

async function scrollPaneTo(page: Page, top: number) {
  await pane(page).evaluate((element, value) => { element.scrollTop = value; }, top);
  // The person moved the pane: a wheel turn is how the page learns it.
  await pane(page).dispatchEvent('wheel', { deltaY: 1 });
}

test.describe('on a wide screen', () => {
  test.use(desktop);

  test('a conversation opens at its newest message from the list, from its address and when another one is picked', async ({ page }) => {
    const desk = await signIn(page, 'Newest Desk');
    const first = await longThread(page, desk, 'Parcel late', 'Message 9: any news today?');
    const second = await longThread(page, desk, 'Invoice wrong', 'Message 9: the total is still wrong.');
    await page.reload();

    await page.getByRole('button', { name: new RegExp(first.subject) }).click();
    await expect(message(page, 'any news today?')).toBeVisible();
    await expectEndInView(page, message(page, 'any news today?'), 'the newest message of the first conversation');

    await page.getByRole('button', { name: new RegExp(second.subject) }).click();
    await expect(message(page, 'the total is still wrong.')).toBeVisible();
    await expectEndInView(page, message(page, 'the total is still wrong.'), 'the newest message of the second conversation');

    await page.goto(`${baseURL}/conversations/${first.id}`);
    await expect(message(page, 'any news today?')).toBeVisible();
    await expectEndInView(page, message(page, 'any news today?'), 'the newest message opened by its address');
  });

  test('a newest message taller than the timeline shows its start', async ({ page }) => {
    const desk = await signIn(page, 'Tall Desk');
    const thread = await longThread(page, desk, 'Long essay', longBody('Newest essay'));
    await page.reload();
    await page.getByRole('button', { name: new RegExp(thread.subject) }).click();
    const newest = message(page, 'Newest essay');
    await expect(newest).toBeVisible();
    await expectStartInView(page, newest, 'a newest message taller than the timeline');
  });

  test('a mail frame that grows after it loads does not push the newest message out of view', async ({ page }) => {
    const desk = await signIn(page, 'Frame Desk');
    const thread = await longThread(page, desk, 'Newsletter', '<p>Newest framed mail</p><div style="height:140px;background:#eee">Banner</div><p>Footer line</p>', true);
    await page.reload();
    await page.getByRole('button', { name: new RegExp(thread.subject) }).click();
    await page.waitForSelector('iframe.email-frame');
    await page.waitForTimeout(1_500);
    await expectEndInView(page, pane(page).locator('article.message').last(), 'the newest framed message');
  });

  test('a reply and a private note just sent are in view, even when the reader had scrolled up', async ({ page }) => {
    const desk = await signIn(page, 'Send Desk');
    const thread = await longThread(page, desk, 'Refund asked', 'Message 9: please refund me.');
    await page.reload();
    await page.getByRole('button', { name: new RegExp(thread.subject) }).click();
    await expect(message(page, 'please refund me.')).toBeVisible();

    await scrollPaneTo(page, 0);
    const box = page.getByLabel('Message body').last();
    await box.fill('Refund sent, Nora.');
    await page.getByRole('button', { name: 'Send reply' }).click();
    await expect(message(page, 'Refund sent, Nora.')).toBeVisible();
    await expectEndInView(page, message(page, 'Refund sent, Nora.'), 'the reply just sent');

    await scrollPaneTo(page, 0);
    await page.getByRole('button', { name: 'Private note', exact: true }).click();
    await box.fill('Checked with finance.');
    await page.getByRole('button', { name: 'Add private note' }).click();
    await expect(message(page, 'Checked with finance.')).toBeVisible();
    await expectEndInView(page, message(page, 'Checked with finance.'), 'the private note just added');
  });

  test('a customer reply that arrives keeps a reader who scrolled up where they are, and a reader at the bottom at the bottom', async ({ page }) => {
    // The inbox looks for new mail every 20 seconds, twice here.
    test.setTimeout(120_000);
    const desk = await signIn(page, 'Arrive Desk');
    const thread = await longThread(page, desk, 'Waiting for news', 'Message 9: still waiting.');
    await page.reload();
    await page.getByRole('button', { name: new RegExp(thread.subject) }).click();
    await expectEndInView(page, message(page, 'still waiting.'), 'the newest message');

    // At the bottom: the next mail comes into view by itself.
    arrive(thread, 'Message 10: hello again?');
    await expect(message(page, 'hello again?')).toBeVisible({ timeout: 45_000 });
    await expectEndInView(page, message(page, 'hello again?'), 'a customer reply that arrived while the reader was at the bottom');

    // Scrolled up: the position does not move when the next one comes.
    await scrollPaneTo(page, 400);
    const kept = await scrollOffset(page);
    arrive(thread, 'Message 11: anyone there?');
    await expect(pane(page).locator('article.message')).toHaveCount(11, { timeout: 45_000 });
    await page.waitForTimeout(500);
    expect(Math.abs((await scrollOffset(page)) - kept)).toBeLessThanOrEqual(1);
  });
});

test.describe('on a phone', () => {
  test.use(phone);

  test('a conversation opens at its newest message from the list and from its address, and the reply just sent is in view', async ({ page }) => {
    const desk = await signIn(page, 'Phone Newest');
    const thread = await longThread(page, desk, 'Parcel late', 'Message 9: any news today?');
    await page.goto(`${baseURL}/?status=all`);
    await page.getByRole('region', { name: 'Conversation list' }).getByRole('button', { name: new RegExp(thread.subject) }).tap();
    await expect(message(page, 'any news today?')).toBeVisible();
    await expectEndInView(page, message(page, 'any news today?'), 'the newest message opened from the list');

    await page.goto(`${baseURL}/conversations/${thread.id}`);
    await expect(message(page, 'any news today?')).toBeVisible();
    await expectEndInView(page, message(page, 'any news today?'), 'the newest message opened by its address');

    await scrollPaneTo(page, 0);
    const box = page.getByLabel('Message body');
    await box.focus();
    await box.fill('On it, Nora.');
    await page.getByRole('button', { name: 'Send reply' }).tap();
    await expect(message(page, 'On it, Nora.')).toBeVisible();
    await expectEndInView(page, message(page, 'On it, Nora.'), 'the reply just sent');
  });
});
