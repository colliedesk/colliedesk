import { createHash } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { ports } from './support/checkout';
import { startFakeZalo } from './support/zalo';

// docs/user/zalo.md: an admin connects a Zalo OA in Settings, adds its tokens, links a group that wrote to
// it to an organization, adds a staff account and sets a command. Zalo itself is this test: it signs a
// group message the way Zalo does (sha256 of app id, body, timestamp and OA secret key) and posts it to the
// channel's webhook. Nothing goes to Zalo: the OA's replies are not part of this journey.
const baseURL = servers.main;

// tipFits hovers a control and says whether its tooltip (data-tip, drawn by ::after) is inside every box that
// clips it sideways: the tip is not an element, so its box is worked out from its computed style.
async function tipFits(page: import('@playwright/test').Page, selector: string) {
  await page.locator(selector).hover();
  return page.evaluate((sel) => {
    const control = document.querySelector(sel) as HTMLElement;
    const style = getComputedStyle(control, '::after');
    const width = parseFloat(style.width);
    const box = control.getBoundingClientRect();
    const [left, right] = style.right === '0px' ? [box.right - width, box.right] : [box.left + box.width / 2 - width / 2, box.left + box.width / 2 + width / 2];
    for (let node = control.parentElement; node; node = node.parentElement) {
      if (getComputedStyle(node).overflowX === 'visible') continue;
      const clip = node.getBoundingClientRect();
      if (left < clip.left - 0.5 || right > clip.right + 0.5) return `${node.className}: ${Math.round(left)}–${Math.round(right)} outside ${Math.round(clip.left)}–${Math.round(clip.right)}`;
    }
    return style.display === 'none' ? 'hidden' : 'fits';
  }, selector);
}

test('zalo channel: an admin connects an OA, adds its tokens, links a group that wrote to an organization, adds staff and a command', async ({ page }) => {
  const suffix = uniqueSuffix();
  // One OA is one channel on the whole server: every run gets its own.
  const oa = `43${Date.now()}${Math.floor(Math.random() * 1000)}`;
  const app = '1188200455';
  const secret = `oa-secret-${suffix}-7c20`;
  await redeemLink(page, createWorkspace(`Zalo Desk ${suffix}`, `admin-${suffix}@example.com`, baseURL));

  await page.getByRole('link', { name: 'Settings' }).click();
  await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Channels' }).click();
  await page.getByRole('link', { name: /Connect Zalo OA/ }).click();
  await page.getByRole('textbox', { name: 'OA ID' }).fill(oa);
  await page.getByRole('textbox', { name: 'App ID' }).fill(app);
  await page.getByLabel('App secret').fill(`app-secret-${suffix}-a1f9`);
  await page.getByLabel('OA secret key').fill(secret);
  await page.getByRole('button', { name: 'Connect Zalo OA' }).click();

  // The channel's page opens on Connection: the webhook to paste, and which secrets are set.
  await expect(page.getByRole('heading', { name: 'Zalo OA', level: 2 })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Connection' })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByText(`${baseURL}/api/v1/channels/zalo/webhook`)).toBeVisible();
  await expect(page.getByText(/^Set · ••••7c20/)).toBeVisible();
  await expect(page.getByText(/^Set · ••••a1f9/)).toBeVisible();
  await expect(page.getByText('Not set: messages arrive, replies are not sent')).toBeVisible();
  await page.getByRole('button', { name: 'Add tokens' }).click();
  const tokens = page.getByRole('dialog', { name: 'Add the tokens of Zalo OA' });
  await tokens.getByLabel('Access token').fill(`access-${suffix}`);
  await tokens.getByLabel('Refresh token').fill(`refresh-${suffix}`);
  await tokens.getByRole('button', { name: 'Save tokens' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Tokens saved.' })).toBeVisible();
  await expect(page.getByText(/^Set · last written/)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Replace tokens' })).toBeVisible();

  // A customer writes in a Zalo group the OA is in.
  const group = `g${suffix.replace(/[^0-9a-z]/gi, '')}`;
  const body = JSON.stringify({ app_id: app, oa_id: oa, event_name: 'user_send_group_text', timestamp: String(Date.now()),
    sender: { id: '6620001029' }, recipient: { id: group }, message: { msg_id: `m-${suffix}`, text: 'Máy in kho 2 không in được' } });
  const timestamp = (JSON.parse(body) as { timestamp: string }).timestamp;
  const signature = createHash('sha256').update(app).update(body).update(timestamp).update(secret).digest('hex');
  const delivered = await page.request.post(`${baseURL}/api/v1/channels/zalo/webhook`, { headers: { 'Content-Type': 'application/json', 'X-ZEvent-Signature': `mac=${signature}` }, data: body });
  expect(delivered.status()).toBe(200);

  // Groups: the group that wrote is listed, and linking it files its conversations under an organization.
  await page.getByRole('tab', { name: 'Groups' }).click();
  const groups = page.getByRole('list', { name: 'Zalo groups' });
  await expect(async () => {
    await page.reload();
    await expect(groups.getByText(`Group ${group}`)).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 15_000 });
  await groups.getByRole('button', { name: 'Link…' }).click();
  const link = page.getByRole('dialog', { name: `Link “Group ${group}” to an organization` });
  await link.getByLabel('Organization').fill('HC Labs');
  await link.getByRole('button', { name: 'Link' }).click();
  await expect(page.getByRole('status').filter({ hasText: `Linked “Group ${group}” to HC Labs.` })).toBeVisible();
  await expect(groups.getByText('HC Labs')).toBeVisible();
  await expect(page.getByText('· 1 group linked')).toBeVisible();

  // Staff: the customer who wrote is offered; a pasted id is added and removed again.
  await page.getByRole('tab', { name: 'Staff' }).click();
  await expect(page.getByText(/No staff accounts yet/)).toBeVisible();
  await page.getByRole('button', { name: 'Add staff account' }).click();
  const add = page.getByRole('dialog', { name: 'Add staff account' });
  await expect(add.getByRole('radio', { name: /uid 6620001029/ })).toBeVisible();
  await add.getByLabel('Or paste a Zalo user id').fill('9930004402');
  await add.getByRole('button', { name: 'Add' }).click();
  const staff = page.getByRole('list', { name: 'Staff accounts' });
  await expect(staff.getByText('uid 9930004402').first()).toBeVisible();
  await staff.getByRole('button', { name: /^Remove / }).click();
  await page.getByRole('alertdialog', { name: /Stop treating .* as staff\?/ }).getByRole('button', { name: 'Remove' }).click();
  await expect(page.getByText(/No staff accounts yet/)).toBeVisible();

  // Pause asks first and keeps what arrives; Resume takes nothing more than a click.
  await page.getByRole('button', { name: 'Pause processing' }).click();
  await page.getByRole('alertdialog', { name: 'Pause processing on Zalo OA?' }).getByRole('button', { name: 'Pause processing' }).click();
  await expect(page.getByText(/Processing is paused since/)).toBeVisible();
  await page.getByRole('button', { name: 'Resume processing' }).click();
  await expect(page.locator('p', { hasText: 'Processing resumed.' })).toBeFocused();
  await expect(page.getByRole('button', { name: 'Pause processing' })).toBeVisible();

  // Messages: a command with its phrase, matched exactly, is saved and in force after a reload.
  await page.getByRole('tab', { name: 'Messages' }).click();
  const open = page.getByRole('group', { name: 'Phrases for Open a request' });
  await open.getByRole('button', { name: 'Add a phrase' }).click();
  await open.getByRole('textbox', { name: 'New phrase for Open a request' }).fill('tạo phiếu');
  await open.getByRole('textbox', { name: 'New phrase for Open a request' }).press('Enter');
  await page.getByRole('combobox', { name: 'Match for Open a request' }).selectOption('exact');
  await page.getByLabel('When a conversation opens').fill('Đã nhận yêu cầu của anh/chị.');
  await page.getByRole('button', { name: 'Save messages' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('group', { name: 'Phrases for Open a request' }).getByText('tạo phiếu')).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Match for Open a request' })).toHaveValue('exact');
  await expect(page.getByLabel('When a conversation opens')).toHaveValue('Đã nhận yêu cầu của anh/chị.');
});

test('zalo channel: a series of its own numbers what the OA opens, the inbox shows the code, and the code finds it', async ({ page }) => {
  const suffix = uniqueSuffix();
  const oa = `44${Date.now()}${Math.floor(Math.random() * 1000)}`;
  const app = '1188200455';
  const secret = `oa-secret-${suffix}`;
  const series = `t${suffix.replace(/[^0-9]/g, '').slice(-8)}`;
  await redeemLink(page, createWorkspace(`Zalo Numbers ${suffix}`, `admin-${suffix}@example.com`, baseURL));
  const year = Number(new Intl.DateTimeFormat('en', { timeZone: 'UTC', year: 'numeric' }).format(new Date()));

  // The series, in Settings → Conversation numbers.
  await page.goto(`${baseURL}/settings/numbers`);
  await page.getByRole('button', { name: 'Add a series' }).click();
  const add = page.getByRole('dialog', { name: 'Add a number series' });
  await add.getByLabel('Name').fill(series);
  await add.getByLabel('Code format').fill('CHD-{yyyy}-{n:04}');
  await add.getByLabel('Next number').fill('142');
  await add.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('list', { name: 'Number series' }).getByText(`Next: CHD-${year}-0142`)).toBeVisible();

  // The OA, and its series on Connection → References.
  await page.goto(`${baseURL}/settings/channels/new/zalo`);
  await page.getByRole('textbox', { name: 'OA ID' }).fill(oa);
  await page.getByRole('textbox', { name: 'App ID' }).fill(app);
  await page.getByLabel('OA secret key').fill(secret);
  await page.getByRole('button', { name: 'Connect Zalo OA' }).click();
  await page.getByLabel('Number series').selectOption(series);
  await expect(page.getByLabel('Number series')).toHaveAccessibleDescription(`Next reference: CHD-${year}-0142`);
  await page.getByRole('button', { name: 'Save series' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();

  // A customer writes one to one: the conversation it opens takes the series' code.
  const body = JSON.stringify({ app_id: app, event_name: 'user_send_text', timestamp: String(Date.now()),
    sender: { id: '7700001029' }, recipient: { id: oa }, message: { msg_id: `m-${suffix}`, text: 'Máy in kho 2 không in được' } });
  const timestamp = (JSON.parse(body) as { timestamp: string }).timestamp;
  const signature = createHash('sha256').update(app).update(body).update(timestamp).update(secret).digest('hex');
  expect((await page.request.post(`${baseURL}/api/v1/channels/zalo/webhook`, { headers: { 'Content-Type': 'application/json', 'X-ZEvent-Signature': `mac=${signature}` }, data: body })).status()).toBe(200);

  const code = `CHD-${year}-0142`;
  await page.goto(`${baseURL}/`);
  await expect(async () => {
    await page.reload();
    await expect(page.locator('.conversation-meta .conv-ref', { hasText: code })).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 20_000 });
  await expect(page.getByRole('button', { name: `Copy reference ${code}` })).toBeVisible();

  // The code, typed another way, finds it; a link with it says so in a chip.
  await page.getByRole('searchbox').first().fill(code.toLowerCase().replace('0142', '142'));
  await expect(page.locator('.conversation-row').first()).toContainText(code);
  await page.goto(`${baseURL}/?ref=${encodeURIComponent(code)}`);
  await expect(page.getByRole('list', { name: 'Active filters' })).toContainText(`Reference: ${code}`);
  await expect(page.locator('.conversation-row')).toHaveCount(1);
});

test('zalo channel: words for Resolved make the header say it tells, the quiet way resolves without a word, at phone width too', async ({ page }) => {
  test.setTimeout(90_000);
  // Zalo is the stand-in the server is started against (COLLIEDESK_E2E_ZALO_URL): what the OA sends lands here.
  const zalo = await startFakeZalo(ports.zalo);
  try {
  const suffix = uniqueSuffix();
  const oa = `45${Date.now()}${Math.floor(Math.random() * 1000)}`;
  const app = '1188200455';
  const secret = `oa-secret-${suffix}`;
  await redeemLink(page, createWorkspace(`Zalo Status ${suffix}`, `admin-${suffix}@example.com`, baseURL));
  await page.goto(`${baseURL}/settings/channels/new/zalo`);
  await page.getByRole('textbox', { name: 'OA ID' }).fill(oa);
  await page.getByRole('textbox', { name: 'App ID' }).fill(app);
  await page.getByLabel('OA secret key').fill(secret);
  await page.getByRole('button', { name: 'Connect Zalo OA' }).click();
  await page.getByRole('button', { name: 'Add tokens' }).click();
  const tokens = page.getByRole('dialog', { name: 'Add the tokens of Zalo OA' });
  await tokens.getByLabel('Access token').fill(`access-${suffix}`);
  await tokens.getByLabel('Refresh token').fill(`refresh-${suffix}`);
  await tokens.getByRole('button', { name: 'Save tokens' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Tokens saved.' })).toBeVisible();

  // Messages: the words for Resolved, and closing after 48 hours (said under the box before saving).
  await page.getByRole('tab', { name: 'Messages' }).click();
  const block = page.getByRole('region', { name: 'When a conversation’s status changes' });
  await block.getByLabel('When set to Resolved').fill('Yêu cầu {ref} đã được xử lý.');
  await block.getByLabel('When set to Pending').fill('Yêu cầu {ref} đang chờ.');
  await block.getByLabel('Close resolved conversations after').fill('48');
  await expect(block.getByText('A conversation that stays resolved for 48 hours closes by itself, quietly: “When set to Closed” is empty.')).toBeVisible();
  await page.getByRole('button', { name: 'Save messages' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  await page.reload();
  await page.getByRole('tab', { name: 'Messages' }).click();
  await expect(page.getByLabel('When set to Resolved')).toHaveValue('Yêu cầu {ref} đã được xử lý.');
  await expect(page.getByLabel('Close resolved conversations after')).toHaveValue('48');

  // A customer writes one to one; the conversation's header says Resolve tells them.
  const body = JSON.stringify({ app_id: app, event_name: 'user_send_text', timestamp: String(Date.now()),
    sender: { id: '7700002031' }, recipient: { id: oa }, message: { msg_id: `m-${suffix}`, text: 'Cần hỗ trợ đổi mật khẩu' } });
  const timestamp = (JSON.parse(body) as { timestamp: string }).timestamp;
  const signature = createHash('sha256').update(app).update(body).update(timestamp).update(secret).digest('hex');
  expect((await page.request.post(`${baseURL}/api/v1/channels/zalo/webhook`, { headers: { 'Content-Type': 'application/json', 'X-ZEvent-Signature': `mac=${signature}` }, data: body })).status()).toBe(200);
  await page.goto(`${baseURL}/`);
  await expect(async () => {
    await page.reload();
    await page.locator('.conversation-row').first().click({ timeout: 1000 });
  }).toPass({ timeout: 20_000 });
  const resolve = page.getByRole('button', { name: /^Resolve and tell the customer on / });
  await expect(resolve).toHaveText('Resolve and tell');
  // Its arrow's tip is read whole: the thread pane clips sideways.
  await page.setViewportSize({ width: 1440, height: 900 });
  expect(await tipFits(page, 'summary[aria-label="More ways to resolve"]')).toBe('fits');
  // So is the tip of the customer message's own menu (Move this message), at every width.
  for (const width of [1440, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    expect(await tipFits(page, '.message-move-menu summary')).toBe('fits');
  }

  // At 390 px nothing scrolls sideways, the quiet menu open or closed.
  await page.setViewportSize({ width: 390, height: 844 });
  const sideways = () => page.evaluate(() => [document.documentElement, ...Array.from(document.querySelectorAll('.timeline-actions, .status-split, .details-panel'))]
    .filter((node) => node.scrollWidth > node.clientWidth).map((node) => node.className || node.tagName));
  expect(await sideways()).toEqual([]);
  await page.setViewportSize({ width: 768, height: 1024 });
  expect(await sideways()).toEqual([]);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator('summary[aria-label="More ways to resolve"]').click();
  const quiet = page.getByRole('button', { name: 'Resolve without telling the customer' });
  await expect(quiet).toBeInViewport({ ratio: 1 });
  expect(await sideways()).toEqual([]);

  // The quiet way resolves it and the channel sends nothing.
  await quiet.click();
  const reopen = page.getByRole('button', { name: /^Reopen/ });
  await expect(reopen).toBeVisible();
  await page.waitForTimeout(6000); // longer than the notice worker's 5 s round
  expect(zalo.sent().filter((item) => item.text?.includes('đã được xử lý'))).toEqual([]);

  // Reopened and resolved with one press, the OA tells the customer, one to one, with the conversation's code.
  await reopen.click();
  await page.getByRole('button', { name: /^Resolve and tell the customer on / }).click();
  await expect(page.getByRole('button', { name: /^Reopen/ })).toBeVisible();
  await expect.poll(() => zalo.sent().filter((item) => item.text?.includes('đã được xử lý')), { timeout: 20_000 })
    .toEqual([expect.objectContaining({ path: '/v3.0/oa/message/cs', recipient: { user_id: '7700002031' }, text: expect.stringMatching(/^Yêu cầu #\d+ đã được xử lý\.$/) })]);

  // The bulk bar and the Messages tab fit a phone too, Tell customers on the first line of the bar.
  const fits = async (selector: string) => (page.evaluate((sel) => [document.documentElement, ...Array.from(document.querySelectorAll(sel))]
    .filter((node) => node.scrollWidth > node.clientWidth).map((node) => node.className || node.tagName), selector));
  await page.goto(`${baseURL}/`);
  await page.getByRole('radio', { name: 'Resolved' }).check();
  await page.locator('.row-check input').first().check();
  await expect(page.getByRole('checkbox', { name: 'Tell customers' })).toBeChecked();
  expect(await fits('.bulk-bar')).toEqual([]);
  expect(await tipFits(page, '.bulk-actions > button:last-child')).toBe('fits');
  for (const width of [768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    expect(await fits('.bulk-bar')).toEqual([]);
    expect(await tipFits(page, '.bulk-actions > button:last-child')).toBe('fits');
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${baseURL}/settings/channels`);
  await page.getByRole('link', { name: /Zalo OA/ }).first().click();
  await page.getByRole('tab', { name: 'Messages' }).click();
  await expect(page.getByLabel('When set to Resolved')).toBeVisible();
  expect(await fits('#status-notices')).toEqual([]);
  } finally {
    await zalo.stop();
  }
});
