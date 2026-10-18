import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// What jsdom cannot show: the email frame's layout and its network behaviour in a real browser.
// Every mail here is synthetic.
const baseURL = servers.main;

async function deskWithMail(page: Page, subject: string, html: string): Promise<string> {
  const suffix = uniqueSuffix();
  const support = `support-${suffix}@acme.example`;
  await redeemLink(page, createWorkspace(`Frame Desk ${suffix}`, `admin-${suffix}@acme.example`, baseURL));
  const headers = { Origin: baseURL };
  expect((await page.request.put(`${baseURL}/api/v1/channels/email`, { headers, data: { support_address: support } })).status()).toBe(200);
  const secret = (await (await page.request.post(`${baseURL}/api/v1/channels/email/secret`, { headers })).json() as { inbound_secret: string }).inbound_secret;
  const inbound = await page.request.post(`${baseURL}/api/v1/channels/email/inbound`, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret },
    data: Buffer.from(['From: Nora Sample <nora@sample.example>', `To: ${support}`, `Subject: ${subject}`, `Message-ID: <${suffix}@sample.example>`,
      'MIME-Version: 1.0', 'Content-Type: text/html; charset=utf-8', '', html].join('\r\n')),
  });
  expect(inbound.status()).toBe(200);
  await page.getByRole('link', { name: 'Inbox' }).click();
  await page.getByRole('button', { name: new RegExp(subject) }).click();
  await page.waitForSelector('iframe.email-frame');
  return suffix;
}

async function frameHeight(page: Page): Promise<number> {
  return page.locator('iframe.email-frame').evaluate((frame) => frame.getBoundingClientRect().height);
}

// A template that sizes itself by the viewport used to make the frame follow its own height upward without end.
const viewportSizedMail = {
  'html and body at 100% height': '<style>html,body{height:100%}</style><p>Short mail</p>',
  'a block with min-height 100vh': '<p>Hi</p><div style="min-height:100vh"></div>',
  'a block with height 100vh and body padding': '<style>body{padding:30px}</style><div style="height:100vh;background:#eee">Hero</div><p>Footer</p>',
};

for (const [name, html] of Object.entries(viewportSizedMail)) {
  test(`email frame: ${name} settles at a sane height and stays there`, async ({ page }) => {
    await deskWithMail(page, `Layout ${name}`, html);
    await page.waitForTimeout(1500);
    const first = await frameHeight(page);
    await page.waitForTimeout(1500);
    const second = await frameHeight(page);
    expect(second).toBe(first);
    expect(first).toBeLessThan(900);
  });
}

test('email frame: mail CSS cannot make the browser call the app API', async ({ page }) => {
  const calls: string[] = [];
  // A request the policy blocks is attempted but never answered; one that reaches the server gets a response.
  page.on('response', (response) => { if (/\/api\/v1\/(export|auth\/invites|billing)\b/.test(response.url())) calls.push(response.url()); });
  await deskWithMail(page, 'CSS probe', '<div style="background-image:url(/api/v1/export)">a</div><div style="background:url(/api/v1/billing)">b</div>'
    + '<style>@import url(/api/v1/auth/invites); p{list-style:url(/api/v1/export?x=1);cursor:url(/api/v1/billing),auto}</style><p>text</p>');
  await page.waitForTimeout(1500);
  expect(calls).toEqual([]);
});

test('email frame: a fixed 650 px mail is scaled into the column, not clipped or scrolled', async ({ page }) => {
  await page.setViewportSize({ width: 1100, height: 800 });
  await deskWithMail(page, 'Wide', '<table width="900" style="width:900px"><tr><td style="width:900px;height:40px;background:#ccc">wide</td></tr></table><p>after</p>');
  await page.waitForTimeout(800);
  const state = await page.locator('iframe.email-frame').evaluate((frame) => {
    const doc = (frame as HTMLIFrameElement).contentDocument!;
    const last = doc.querySelector('p')!.getBoundingClientRect();
    return { client: frame.clientWidth, scroll: doc.documentElement.scrollWidth, cut: last.bottom > frame.clientHeight + 1 };
  });
  expect(state.scroll).toBeLessThanOrEqual(state.client + 1);
  expect(state.cut).toBe(false);
});
