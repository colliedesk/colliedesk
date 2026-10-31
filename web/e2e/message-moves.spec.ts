import { expect, test } from '@playwright/test';
import { createWorkspace, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import type { ChatServer } from './support/chat';
import { addLoopbackChannel, sendLoopback, startChatServer } from './support/chat';

// docs/user/inbox.md "Move messages to another conversation", on the real server: the rules add a chat
// reply to its conversation, an agent splits it into a new one and undoes it, then takes it back to Review.
let chat: ChatServer;

test.beforeAll(async () => { chat = await startChatServer(); });
test.afterAll(async () => { await chat?.stop(); });

test('message moves: the rules add a reply, an agent splits it out, undo puts it back, and it goes back to Review as a card', async ({ page }) => {
  const { origin } = chat;
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Moves Desk ${suffix}`, `admin-${suffix}@example.com`, origin, chat.databaseURL));
  const account = `moves-${suffix}`;
  await addLoopbackChannel(page.request, origin, account);

  // A first message opens a conversation by the group's rules; a reply to it is added there.
  await sendLoopback(page.request, origin, account, [{ id: `a1-${suffix}`, from: 'a', name: 'Minh', group: 'g1', text: 'The stock export fails' }]);
  let conversation = '';
  await expect.poll(async () => {
    const page1 = await (await page.request.get(`${origin}/api/v1/conversations?page=1&page_size=50`)).json() as { items: { id: string }[] };
    conversation = page1.items[0]?.id ?? '';
    return conversation;
  }, { timeout: 30_000 }).not.toBe('');
  await sendLoopback(page.request, origin, account, [{ id: `a2-${suffix}`, from: 'a', name: 'Minh', group: 'g1', text: 'Also the invoice total is wrong', reference: `a1-${suffix}` }]);
  await expect.poll(async () => ((await (await page.request.get(`${origin}/api/v1/conversations/${conversation}/messages`)).json()) as { items: unknown[] }).items.length, { timeout: 30_000 }).toBe(2);

  await page.goto(`${origin}/conversations/${conversation}`);
  const timeline = page.getByRole('region', { name: 'Timeline' });
  const reply = timeline.getByRole('article').filter({ hasText: 'Also the invoice total is wrong' });
  await expect(reply).toContainText('Added: a reply to a message here');

  // Split the reply into a conversation of its own.
  await reply.getByLabel('Move this message', { exact: true }).first().click();
  await reply.getByRole('button', { name: 'Split into a new conversation…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Split into a new conversation' });
  await dialog.getByLabel('Subject of the new conversation').fill('Invoice total');
  await dialog.getByRole('button', { name: 'Split' }).click();
  const moves = timeline.getByRole('region', { name: 'Moves' });
  await expect(moves).toContainText('You split 1 message into');
  await expect(moves.getByRole('link', { name: 'Invoice total' })).toBeVisible();
  await expect(timeline.getByText('Also the invoice total is wrong')).toHaveCount(0);

  // Undo brings it back.
  await moves.getByRole('button', { name: 'Undo' }).click();
  await expect(timeline.getByText('Also the invoice total is wrong')).toBeVisible();
  await expect(moves).toContainText('Undone by You');

  // Back to Review: the reply leaves the conversation and waits as a card that says where it came from.
  await timeline.getByRole('article').filter({ hasText: 'Also the invoice total is wrong' }).getByLabel('Move this message', { exact: true }).first().click();
  await page.getByRole('button', { name: 'Take back to Review' }).click();
  await expect(moves).toContainText('You took 1 message back to Review');
  await moves.getByRole('link', { name: 'Open Review' }).click();
  await expect(page.getByRole('article', { name: 'Minh' })).toContainText(`Taken back from a conversation by admin-${suffix}@example.com`);
});
