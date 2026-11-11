import { expect, test } from '@playwright/test';
import { createWorkspace, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import type { ChatServer } from './support/chat';
import { addLoopbackChannel, holdGroupsForReview, sendLoopback, startChatServer, waitForCards } from './support/chat';

// docs/user/channels.md "Intake queue", on the real server: what customers say in a chat group becomes
// cards, one per customer, decided by a person.
let chat: ChatServer;

test.beforeAll(async () => { chat = await startChatServer(); });
test.afterAll(async () => { await chat?.stop(); });

test('intake queue: two customers writing in turn in a group are two cards; a person opens one and sets the other aside', async ({ page }) => {
  const { origin } = chat;
  const headers = { Origin: origin };
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Intake Desk ${suffix}`, `admin-${suffix}@example.com`, origin, chat.databaseURL));
  const account = `intake-${suffix}`;
  const channel = await addLoopbackChannel(page.request, origin, account);
  await holdGroupsForReview(page.request, origin, channel);

  await sendLoopback(page.request, origin, account, [
    { id: `a1-${suffix}`, from: 'a', name: 'Minh', group: 'g1', text: 'The stock export fails' },
    { id: `b1-${suffix}`, from: 'b', name: 'Lan', group: 'g1', text: 'Meeting tomorrow at 9' },
    { id: `a2-${suffix}`, from: 'a', name: 'Minh', group: 'g1', text: 'Since this morning' },
  ]);
  const ids = await waitForCards(page.request, origin, channel, 2);
  const cards = await Promise.all(ids.map(async (id) => (await page.request.get(`${origin}/api/v1/intake/gatherings/${id}`)).json() as Promise<{
    id: string; item_count: number; rules: { basis: string; rule: string }; place: { kind: string } | null; commands: string[]; sender: { name: string | null };
  }>));
  const minh = cards.find((card) => card.sender.name === 'Minh')!;
  const lan = cards.find((card) => card.sender.name === 'Lan')!;
  expect(minh).toMatchObject({ item_count: 2, rules: { basis: 'default', rule: 'review_hold' }, place: { kind: 'group' } });
  expect(lan.item_count).toBe(1);
  expect(minh.commands.length).toBeGreaterThan(0);
  expect(await (await page.request.get(`${origin}/api/v1/intake/summary`)).json()).toMatchObject({ available: true, waiting: 2 });

  const opened = await page.request.post(`${origin}/api/v1/intake/gatherings/${minh.id}/decide`, { headers, data: { action: 'create', subject: 'Stock export' } });
  expect(opened.status()).toBe(200);
  const { conversation_id: conversation } = await opened.json() as { conversation_id: string };
  expect(await (await page.request.get(`${origin}/api/v1/conversations/${conversation}`)).json()).toMatchObject({ subject: 'Stock export' });
  expect((await page.request.post(`${origin}/api/v1/intake/gatherings/${lan.id}/decide`, { headers, data: { action: 'ignore', ignore_reason: 'chat' } })).status()).toBe(200);
  expect(await waitForCards(page.request, origin, channel, 1, 'ignored')).toEqual([lan.id]);
  expect(await (await page.request.get(`${origin}/api/v1/intake/summary`)).json()).toMatchObject({ waiting: 0 });
});
