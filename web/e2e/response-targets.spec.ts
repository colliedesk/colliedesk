import { createHash } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// docs/user/response-targets.md: an admin sets business hours and a target in Settings; a conversation that
// opens is due soon at once, its row and header say so, Targets in the details say how long is left, and
// the Targets filter finds it. Zalo is this test: a signed one-to-one message opens the conversation.
const baseURL = servers.main;

test('response targets: an admin turns them on, a new conversation is due soon on its row, in its header and details, and the filter finds it', async ({ page }) => {
  const suffix = uniqueSuffix();
  const oa = `45${Date.now()}${Math.floor(Math.random() * 1000)}`;
  const app = '1188200455';
  const secret = `oa-secret-${suffix}`;
  await redeemLink(page, createWorkspace(`Targets Desk ${suffix}`, `admin-${suffix}@example.com`, baseURL));

  await page.goto(`${baseURL}/settings/targets`);
  // Open around the clock, so the target runs whenever this test does.
  const hours = page.getByRole('region', { name: 'Business hours' });
  for (const day of ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday']) {
    const open = hours.getByLabel(`${day} open`);
    if (!(await open.isChecked())) await open.check();
    await hours.getByLabel(`${day} from`).fill('00:00');
    await hours.getByLabel(`${day} to`).fill('23:59');
  }
  await hours.getByRole('button', { name: 'Save business hours' }).click();
  await expect(hours.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  const targets = page.getByRole('region', { name: 'Targets' });
  await targets.getByLabel('First reply target for Normal, in business hours').fill('1');
  await targets.getByLabel('Due soon, in business hours before the target').fill('1');
  await targets.getByRole('button', { name: 'Save targets' }).click();
  await expect(targets.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  await page.getByRole('region', { name: 'Targets on' }).getByRole('switch', { name: 'Measure response targets' }).check();
  await expect(page.getByRole('region', { name: 'Targets on' }).getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();

  // A channel and a customer's message.
  // Added as the signed-in admin's browser does (same origin), not what this test is about.
  const added = await page.evaluate(async ({ oa: oaID, app: appID, secret: key, suffix: unique }) => {
    const channel = await fetch('/api/v1/channels', { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': `zalo-${unique}` },
      body: JSON.stringify({ type: 'zalo', enabled: true, config: { oa_id: oaID, app_id: appID } }) });
    if (!channel.ok) return `channel ${channel.status}`;
    const { id } = await channel.json() as { id: string };
    const stored = await fetch(`/api/v1/channels/${id}/credentials`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'oa_secret_key', value: key }) });
    return stored.ok ? 'ok' : `credential ${stored.status}`;
  }, { oa, app, secret, suffix });
  expect(added).toBe('ok');
  const body = JSON.stringify({ app_id: app, event_name: 'user_send_text', timestamp: String(Date.now()),
    sender: { id: '7800001029' }, recipient: { id: oa }, message: { msg_id: `m-${suffix}`, text: 'Máy in không in được' } });
  const timestamp = (JSON.parse(body) as { timestamp: string }).timestamp;
  const signature = createHash('sha256').update(app).update(body).update(timestamp).update(secret).digest('hex');
  expect((await page.request.post(`${baseURL}/api/v1/channels/zalo/webhook`, { headers: { 'Content-Type': 'application/json', 'X-ZEvent-Signature': `mac=${signature}` }, data: body })).status()).toBe(200);

  await page.goto(`${baseURL}/`);
  const mark = page.locator('.conversation-meta .sla-mark');
  await expect(async () => {
    await page.reload();
    await expect(mark).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 20_000 });
  await expect(mark).toHaveText(/^Due /);
  await expect(mark).toHaveAccessibleName(/^First reply due .* today, counted in business hours$/);

  await page.locator('.conversation-row').first().click();
  await expect(page.locator('.timeline-meta .sla-mark')).toHaveText(/^Reply by /);
  // Below 75 rem the details are a drawer behind Show details.
  const toggle = page.getByRole('button', { name: 'Show details' });
  if (await toggle.isVisible()) await toggle.click();
  const details = page.getByRole('region', { name: 'Targets' });
  await expect(details).toContainText(/Due .* today/);
  await expect(details).toContainText(/of business time left/);

  await page.goto(`${baseURL}/?sla=due_soon`);
  await expect(page.getByRole('list', { name: 'Active filters' })).toContainText('Targets: Due soon');
  await expect(page.locator('.conversation-row')).toHaveCount(1);
  await page.goto(`${baseURL}/?sla=overdue`);
  await expect(page.locator('.conversation-row')).toHaveCount(0);
});
