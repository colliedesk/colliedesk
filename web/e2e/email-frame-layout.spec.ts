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
  // The fixture arrived out of band. Fetch it now instead of waiting for the 20s activity poll.
  await page.reload();
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

// A white logo on a dark body is blank when the mail's own body colour is lost.
test('email frame: the mail body keeps its own background colour', async ({ page }) => {
  await deskWithMail(page, 'Dark body', '<body bgcolor="#202124" style="color:#ffffff"><p>White text</p></body>');
  const colours = await page.locator('iframe.email-frame').evaluate((frame) => {
    const doc = (frame as HTMLIFrameElement).contentDocument!;
    const style = doc.defaultView!.getComputedStyle(doc.body);
    return { background: style.backgroundColor, color: style.color };
  });
  expect(colours).toEqual({ background: 'rgb(32, 33, 36)', color: 'rgb(255, 255, 255)' });
});

// Plain text inside a div (a chat note) has newlines and no markup for them; the sender saw lines.
test('email frame: text with line breaks shows as lines', async ({ page }) => {
  await deskWithMail(page, 'Chat note', '<div>First line\n\nAttachments:\n- file: <a href="https://sample.example/a.jpg">https://sample.example/a.jpg</a></div>');
  const lines = await page.locator('iframe.email-frame').evaluate((frame) => {
    const doc = (frame as HTMLIFrameElement).contentDocument!;
    const range = doc.createRange();
    range.selectNodeContents(doc.querySelector('div')!);
    return new Set(Array.from(range.getClientRects()).map((rect) => Math.round(rect.top))).size;
  });
  expect(lines).toBeGreaterThanOrEqual(4);
});

// A brand logo is often a CSS background, not an <img>. It goes through the proxy like any other image, and the
// browser never tries to reach the sender itself (not even to be blocked).
test('email frame: CSS background images are fetched through the proxy, never directly from the sender', async ({ page }) => {
  const requests: string[] = [];
  page.on('request', (request) => requests.push(request.url()));
  await deskWithMail(page, 'CSS logo', '<style>@import url(https://css-host.invalid/x.css);.hero{background-image:url("https://cdn.invalid/hero.png")}</style>'
    + '<div class="hero" style="width:80px;height:40px">a</div><table><tr><td style="background:url(https://logo-host.invalid/logo.png) no-repeat" width="80" height="40">x</td></tr></table><p>text</p>');
  await page.waitForTimeout(2000);
  const direct = requests.filter((url) => new URL(url).hostname.endsWith('.invalid'));
  expect(direct, 'the browser tried to reach the sender').toEqual([]);
  const proxied = requests.filter((url) => new URL(url).pathname === '/api/v1/mail-image');
  expect(proxied.length).toBeGreaterThanOrEqual(2);
});

// On a phone the three navigation links show whole words: a label cut in the middle ("Settir") reads as broken.
test('narrow screen: the navigation labels are not cut off', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await deskWithMail(page, 'Narrow nav', '<p>Hello</p>');
  const rail = await page.locator('.rail-links').evaluate((element) => ({ scroll: element.scrollWidth, client: element.clientWidth }));
  expect(rail.scroll).toBeLessThanOrEqual(rail.client);
});
