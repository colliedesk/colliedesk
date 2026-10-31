import { createHash } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// docs/user/zalo.md: an admin connects a Zalo OA in Settings, adds its tokens, links a group that wrote to
// it to an organization, adds a staff account and sets a command. Zalo itself is this test: it signs a
// group message the way Zalo does (sha256 of app id, body, timestamp and OA secret key) and posts it to the
// channel's webhook. Nothing goes to Zalo: the OA's replies are not part of this journey.
const baseURL = servers.main;

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
