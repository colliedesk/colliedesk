import { createHash } from 'node:crypto';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
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
  // Measuring starts from the off card; the editor shows once it is on.
  const turnOn = page.getByRole('button', { name: 'Turn on response targets' });
  await expect(turnOn).toBeEnabled();
  await turnOn.click();
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
  await targets.getByLabel('First reply target for Normal, in business time').fill('1');
  await targets.getByLabel('Due soon, in business time before the target').fill('1');
  await targets.getByRole('button', { name: 'Save targets' }).click();
  await expect(targets.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();

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
  const mark = page.locator('.conversation-row .row-due');
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
  // Due is one row of the details; its menu holds each clock in full.
  await page.locator('.due-pick summary').click();
  const details = page.getByRole('region', { name: 'Targets' });
  await expect(details).toContainText(/Due .* today/);
  await expect(details).toContainText(/of business time left/);

  await page.goto(`${baseURL}/?sla=due_soon`);
  await expect(page.getByRole('list', { name: 'Active filters' })).toContainText('Targets: Due soon');
  await expect(page.locator('.conversation-row')).toHaveCount(1);
  await page.goto(`${baseURL}/?sla=overdue`);
  await expect(page.locator('.conversation-row')).toHaveCount(0);
});

// What the API holds, read as the signed-in browser does.
const stored = (page: Page) => page.evaluate(async () => (await fetch('/api/v1/response-targets')).json() as Promise<{
  enabled: boolean; hours: { day: string; open: boolean; from?: string; to?: string }[]; warn_before_minutes: number;
  targets: Record<string, { first_response_minutes: number | null; resolution_minutes: number | null }> }>);
const openDesk = async (page: Page, label: string) => {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`${label} ${suffix}`, `admin-${suffix}@example.com`, baseURL));
  await page.goto(`${baseURL}/settings/targets`);
};
const editor = (page: Page) => page.getByRole('region', { name: /^(Business hours|Holidays|Targets|Alerts)$/ });

test('response targets off: one card and no editor; turning on with nothing saved starts Monday to Friday, 09:00 to 18:00 with the setup flow\'s targets', async ({ page }) => {
  await openDesk(page, 'Targets Off Desk');
  const card = page.getByRole('region', { name: 'Response targets are off' });
  await expect(card).toBeVisible();
  await expect(card).toContainText('Starts with: Monday to Friday, 09:00 to 18:00');
  await expect(card).toContainText('first reply Urgent 1 h · High 2 h · Normal 8 h · Low 1 day');
  await expect(editor(page)).toHaveCount(0);
  await expect(page.getByLabel('Monday open')).toHaveCount(0);
  await expect(page.getByRole('switch', { name: 'Measure response targets' })).toHaveCount(0);
  expect((await stored(page)).enabled).toBe(false);

  const turnOn = card.getByRole('button', { name: 'Turn on response targets' });
  await expect(turnOn).toBeEnabled();
  await turnOn.click();
  await expect(page.getByRole('region', { name: 'Business hours' })).toBeVisible();
  const doc = await stored(page);
  expect(doc.enabled).toBe(true);
  expect(doc.hours.map((day) => [day.day, day.open])).toEqual([['mon', true], ['tue', true], ['wed', true], ['thu', true], ['fri', true], ['sat', false], ['sun', false]]);
  for (const day of doc.hours.slice(0, 5)) expect([day.from, day.to]).toEqual(['09:00', '18:00']);
  // A Normal message is due in 8 business hours; 30 minutes before, whoever owns it is warned.
  expect(doc.targets.normal.first_response_minutes).toBe(480);
  expect(doc.warn_before_minutes).toBe(30);
});

test('response targets off and on again: hours and targets saved earlier are kept, and the off card names them', async ({ page }) => {
  await openDesk(page, 'Targets Keep Desk');
  await page.getByRole('button', { name: 'Turn on response targets' }).click();
  const hours = page.getByRole('region', { name: 'Business hours' });
  await hours.getByLabel('Monday from').fill('07:30');
  await hours.getByRole('button', { name: 'Save business hours' }).click();
  await expect(hours.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  const targets = page.getByRole('region', { name: 'Targets' });
  await targets.getByLabel('First reply target for Normal, in business time').fill('3');
  await targets.getByRole('button', { name: 'Save targets' }).click();
  await expect(targets.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  const before = await stored(page);

  // Off keeps every saved value, and the card says which hours the next turn-on uses.
  await page.getByRole('switch', { name: 'Measure response targets' }).uncheck();
  const card = page.getByRole('region', { name: 'Response targets are off' });
  await expect(card).toContainText('Starts with your saved hours: Monday, 07:30 to 18:00; Tuesday to Friday, 09:00 to 18:00');
  await expect(card).toContainText('Normal 3 h');
  await expect(editor(page)).toHaveCount(0);
  expect(await stored(page)).toMatchObject({ enabled: false, hours: before.hours, targets: before.targets });

  await card.getByRole('button', { name: 'Turn on response targets' }).click();
  await expect(page.getByRole('region', { name: 'Business hours' })).toBeVisible();
  expect(await stored(page)).toMatchObject({ enabled: true, hours: before.hours, targets: before.targets, warn_before_minutes: before.warn_before_minutes });
});

test.describe('on a phone', () => {
  test.use({ viewport: { width: 390, height: 844 } });
  test('the off card fits the screen and its button is at least 44 px tall', async ({ page }) => {
    await openDesk(page, 'Targets Phone Desk');
    const card = page.getByRole('region', { name: 'Response targets are off' });
    const button = card.getByRole('button', { name: 'Turn on response targets' });
    await expect(button).toBeEnabled();
    expect((await button.boundingBox())?.height).toBeGreaterThanOrEqual(44);
    const fits = await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);
    expect(fits).toBe(true);
  });
});
