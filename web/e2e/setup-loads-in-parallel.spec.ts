import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// The setup guide and the setup flow's email step each need data and chunks. They must be asked for together:
// a request that leaves only after another one came back costs a round trip on every visit from far away.
// A round trip of 150 ms is added to every request, so a chain shows as steps of at least that size.
const baseURL = servers.mail;
const roundTrip = 150;

interface Seen { url: string; started: number; finished?: number }

function record(page: Page): Seen[] {
  const seen: Seen[] = [];
  const bySeenRequest = new Map<unknown, Seen>();
  page.on('request', (request) => {
    const entry = { url: new URL(request.url()).pathname, started: Date.now() };
    seen.push(entry);
    bySeenRequest.set(request, entry);
  });
  page.on('requestfinished', (request) => { const entry = bySeenRequest.get(request); if (entry) entry.finished = Date.now(); });
  return seen;
}

const first = (seen: Seen[], pattern: RegExp) => seen.find((entry) => pattern.test(entry.url));

async function slowNetwork(page: Page) {
  const session = await page.context().newCDPSession(page);
  await session.send('Network.enable');
  await session.send('Network.emulateNetworkConditions', { offline: false, latency: roundTrip, downloadThroughput: -1, uploadThroughput: -1 });
}

test('the setup guide asks for its data and its chunk together, and the email step asks for its list and its chunks together', async ({ page }) => {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Parallel Desk ${suffix}`, `admin-${suffix}@acme.example`, baseURL));
  await slowNetwork(page);

  // The inbox: the guide's data is not asked for after the session, and its chunk not after the data.
  const inbox = record(page);
  await page.goto(baseURL);
  const guide = page.getByRole('region', { name: 'Finish setting up' });
  await expect(guide).toBeVisible();
  const me = first(inbox, /\/api\/v1\/auth\/me$/);
  const setup = first(inbox, /\/api\/v1\/workspace\/setup$/);
  const checklist = first(inbox, /setup-checklist-.*\.js$/);
  expect(me, 'the session request').toBeDefined();
  expect(setup, 'the setup data request').toBeDefined();
  expect(checklist, 'the checklist chunk request').toBeDefined();
  expect(setup!.started, 'setup data left before the session came back').toBeLessThan(me!.finished!);
  expect(checklist!.started, 'the chunk left before the data came back').toBeLessThan(setup!.finished!);

  // The email step of the setup flow: opened from the guide, everything it needs leaves at the click.
  const email = record(page);
  const clicked = Date.now();
  await guide.getByRole('link', { name: /Receive customer email/ }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Receive customer email' })).toBeVisible();
  const route = first(email, /\/setup-flow-.*\.js$/);
  const channels = first(email, /\/api\/v1\/channels$/);
  const panels = first(email, /\/panels-.*\.js$/);
  expect(route, 'the flow chunk request').toBeDefined();
  expect(channels, 'the channel list request').toBeDefined();
  expect(panels, 'the panels chunk request').toBeDefined();
  expect(channels!.started, 'the list left before the flow chunk came back').toBeLessThan(route!.finished!);
  expect(panels!.started, 'the panels left before the flow chunk came back').toBeLessThan(route!.finished!);
  for (const [name, entry] of [['flow chunk', route], ['channel list', channels], ['panels chunk', panels]] as const) {
    expect(entry!.started - clicked, `${name} left within one round trip of the click`).toBeLessThan(roundTrip);
  }
});
