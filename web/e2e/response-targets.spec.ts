import { createHash } from 'node:crypto';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// docs/user/response-targets.md: an admin sets business hours and a target in Settings; a conversation that
// opens is due soon at once, its row and header say so, Targets in the details say how long is left, and
// the Targets filter finds it. Zalo is this test: a signed one-to-one message opens the conversation.
const baseURL = servers.main;

// The test's hours are open 00:00 to 23:59 every day and the first reply target is 1 h, so a message that
// arrives within the last hour of the agent's day is due the next calendar day (the clock stops at 23:59 and
// goes on at 00:00): its row reads "Due Sat 12:02 AM", not "today". The case is decided here from the
// browser's own clock and that rule, never from what the product shows. The page and the product share one
// machine, so one clock; the agent's zone is the browser's (a workspace starts in its creator's browser zone).
const targetSeconds = 60 * 60;
const closeSeconds = 23 * 3600 + 59 * 60;
// Within this many seconds of the turning point, the product's receipt of the message and the test's clock
// could fall on different sides of it: the test waits it out.
const edgeMarginSeconds = 30;
const localClock = (page: Page) => page.evaluate(() => {
  const at = (date: Date, options: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat('en-US', { hourCycle: 'h23', ...options }).formatToParts(date);
  const now = new Date();
  const part = (type: string) => Number(at(now, { hour: '2-digit', minute: '2-digit', second: '2-digit' }).find((item) => item.type === type)?.value);
  // Two hours on is the next calendar day whenever the message is in the last hour of its day.
  const tomorrow = at(new Date(now.getTime() + 2 * 3600 * 1000), { weekday: 'short' }).find((item) => item.type === 'weekday')?.value ?? '';
  return { secondsOfDay: part('hour') * 3600 + part('minute') * 60 + part('second'), tomorrow };
});

test('response targets: an admin turns them on, a new conversation is due soon on its row, in its header and details, and the filter finds it', async ({ page }) => {
  test.setTimeout(120_000);
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
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(hours.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  const targets = page.getByRole('region', { name: 'Targets' });
  await targets.getByLabel('First reply target for Normal, in business time').fill('1');
  await targets.getByLabel('Due soon, in business time before the target').fill('1');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
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
  // Not at the turning point: wait until the case is clear, then decide it from the clock.
  let clock = await localClock(page);
  while (Math.abs(clock.secondsOfDay - (closeSeconds - targetSeconds)) < edgeMarginSeconds) {
    await page.waitForTimeout(1000);
    clock = await localClock(page);
  }
  const dueToday = clock.secondsOfDay + targetSeconds <= closeSeconds;
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
  // Due today reads "today"; due on the next day names that weekday and a time in the first hour after midnight.
  await expect(mark).toHaveAccessibleName(dueToday ? /^First reply due .* today, counted in business hours$/
    : new RegExp(`^First reply due ${clock.tomorrow} (12:\\d{2}|1:00) AM, counted in business hours$`));

  await page.locator('.conversation-row').first().click();
  await expect(page.locator('.timeline-meta .sla-mark')).toHaveText(/^Reply by /);
  // Below 75 rem the details are a drawer behind Show details.
  const toggle = page.getByRole('button', { name: 'Show details' });
  if (await toggle.isVisible()) await toggle.click();
  // Due is one row of the details; its menu holds each clock in full.
  await page.locator('.due-pick summary').click();
  const details = page.getByRole('region', { name: 'Targets' });
  await expect(details).toContainText(dueToday ? /Due .* today/ : new RegExp(`Due ${clock.tomorrow} (12:\\d{2}|1:00) AM`));
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
  timezone: string | null; holidays: { date: string; name: string }[]; channel_ids: string[] | null; alert_email: boolean;
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
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(hours.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  const targets = page.getByRole('region', { name: 'Targets' });
  await targets.getByLabel('First reply target for Normal, in business time').fill('3');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(targets.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  // Values on every part of the document, so a turn-on that dropped one would show.
  // The zone is committed on Enter, where the person stops.
  await hours.getByLabel('Time zone').selectOption('Europe/Paris');
  await hours.getByLabel('Time zone').press('Enter');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(hours.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  // The Saved line of the first save may still be up: the API says when this one is stored.
  await expect.poll(async () => (await stored(page)).timezone).toBe('Europe/Paris');
  const holidays = page.getByRole('region', { name: 'Holidays' });
  await holidays.getByLabel('Date').fill('2027-01-01');
  await holidays.getByLabel('Name').fill('New Year');
  await holidays.getByRole('button', { name: 'Add holiday' }).click();
  await expect(holidays.getByText('New Year')).toBeVisible();
  const alerts = page.getByRole('region', { name: 'Alerts' });
  await alerts.getByRole('switch', { name: 'Email reminders and overdue alerts' }).uncheck();
  // A switch saves when it is flipped, and the page says so with a toast.
  await expect(page.getByRole('status').filter({ hasText: 'Saved' }).filter({ has: page.getByRole('button', { name: 'Undo' }) })).toBeVisible();
  const before = await stored(page);
  expect(before).toMatchObject({ timezone: 'Europe/Paris', holidays: [{ date: '2027-01-01', name: 'New Year' }], alert_email: false });

  // Off keeps every saved value, and the card says which hours the next turn-on uses.
  // The switch leaves the page once measuring is off: click it, since uncheck() looks for it again to check its state.
  await page.getByRole('switch', { name: 'Measure response targets' }).click();
  const card = page.getByRole('region', { name: 'Response targets are off' });
  await expect(card).toContainText('Starts with your saved hours: Monday, 07:30 to 18:00; Tuesday to Friday, 09:00 to 18:00');
  await expect(card).toContainText('Normal 3 h');
  await expect(editor(page)).toHaveCount(0);
  expect(await stored(page)).toMatchObject({ enabled: false, hours: before.hours, targets: before.targets, holidays: before.holidays, channel_ids: before.channel_ids, alert_email: before.alert_email, timezone: before.timezone });

  await card.getByRole('button', { name: 'Turn on response targets' }).click();
  await expect(page.getByRole('region', { name: 'Business hours' })).toBeVisible();
  expect(await stored(page)).toMatchObject({ enabled: true, hours: before.hours, targets: before.targets, warn_before_minutes: before.warn_before_minutes,
    holidays: before.holidays, channel_ids: before.channel_ids, alert_email: before.alert_email, timezone: before.timezone });
});

test('response targets off: a channel deleted since is refused on turn-on, the editor opens to fix the scope, and measuring turns on after', async ({ page }) => {
  await openDesk(page, 'Targets Scope Desk');
  // Two channels, so Applies to shows; the targets are limited to the first, measured, then turned off.
  const made = await page.evaluate(async () => {
    const out: { id: string; name: string }[] = [];
    for (const oa of [`46${Date.now()}1`, `46${Date.now()}2`]) {
      const response = await fetch('/api/v1/channels', { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': `scope-${oa}` },
        body: JSON.stringify({ type: 'zalo', enabled: true, config: { oa_id: oa, app_id: '1188200456' } }) });
      out.push(await response.json() as { id: string; name: string });
    }
    return out;
  });
  const [first, second] = made;
  await page.reload();
  await page.getByRole('button', { name: 'Turn on response targets' }).click();
  const applies = page.getByRole('region', { name: 'Applies to' });
  await applies.getByRole('checkbox', { name: second.name, exact: true }).uncheck();
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(applies.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  expect((await stored(page)).channel_ids).toEqual([first.id]);
  await page.getByRole('switch', { name: 'Measure response targets' }).click();
  await expect(page.getByRole('region', { name: 'Response targets are off' })).toBeVisible();

  // The first channel goes away while measuring is off.
  const deleted = await page.evaluate(async (id) => (await fetch(`/api/v1/channels/${id}`, { method: 'DELETE', headers: { 'Idempotency-Key': `delete-${id}` } })).status, first.id);
  expect(deleted).toBeLessThan(300);
  await page.reload();
  const turnOn = page.getByRole('button', { name: 'Turn on response targets' });
  await expect(turnOn).toBeEnabled();
  await turnOn.click();

  // The refusal is shown beside the scope, nothing was dropped, and measuring is still off.
  const scope = page.getByRole('region', { name: 'Applies to' });
  await expect(scope.getByText('This workspace has no such channel.')).toBeVisible();
  await expect(scope.getByRole('checkbox', { name: /^Deleted channel/ })).toBeChecked();
  expect(await stored(page)).toMatchObject({ enabled: false, channel_ids: [first.id] });
  await expect(page.getByRole('region', { name: 'Response targets are off' })).toBeVisible();

  await scope.getByRole('checkbox', { name: /^Deleted channel/ }).uncheck();
  await scope.getByRole('checkbox', { name: second.name, exact: true }).check();
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(scope.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();
  // The one live channel ticked is every channel.
  expect((await stored(page)).channel_ids).toBeNull();
  await page.getByRole('button', { name: 'Turn on response targets' }).click();
  await expect(page.getByRole('switch', { name: 'Measure response targets' })).toBeChecked();
  expect(await stored(page)).toMatchObject({ enabled: true, channel_ids: null });
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
