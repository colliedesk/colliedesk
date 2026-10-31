import { expect, test } from '@playwright/test';
import { createWorkspace, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import type { ChatServer } from './support/chat';
import { addLoopbackChannel, holdGroupsForReview, sendLoopback, startChatServer, waitForCards } from './support/chat';

// docs/user/review.md, on the real server: cards of a chat group wait on Review, a person opens one as a
// conversation, sets the other aside, finds both under Decided and puts the second back.
let chat: ChatServer;

test.beforeAll(async () => { chat = await startChatServer(); });
test.afterAll(async () => { await chat?.stop(); });

test('review: the rail counts the cards, a person opens one as a conversation, sets one aside and puts it back', async ({ page }) => {
  const { origin } = chat;
  const suffix = uniqueSuffix();
  const admin = `admin-${suffix}@example.com`;
  await redeemLink(page, createWorkspace(`Review Desk ${suffix}`, admin, origin, chat.databaseURL));
  const account = `review-${suffix}`;
  const channel = await addLoopbackChannel(page.request, origin, account);
  await holdGroupsForReview(page.request, origin, channel);
  await sendLoopback(page.request, origin, account, [
    { id: `a1-${suffix}`, from: 'a', name: 'Minh', group: 'g1', text: 'The stock export fails' },
    { id: `b1-${suffix}`, from: 'b', name: 'Lan', group: 'g1', text: 'Meeting tomorrow at 9' },
    { id: `a2-${suffix}`, from: 'a', name: 'Minh', group: 'g1', text: 'Since this morning' },
  ]);
  await waitForCards(page.request, origin, channel, 2);

  // The rail shows Review with what waits once the summary is read again; a reload reads it now.
  await page.reload();
  await page.getByRole('link', { name: 'Review, 2 waiting' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Review' })).toBeVisible();
  const minh = page.getByRole('article', { name: 'Minh' });
  const lan = page.getByRole('article', { name: 'Lan' });
  await expect(minh).toContainText('No command');
  await expect(minh).toContainText('2 messages');

  // Open Minh's card: both messages, the subject from the first line; Create opens the conversation.
  await minh.getByRole('button', { name: 'Decide' }).click();
  await expect(minh).toContainText('Since this morning');
  await expect(minh.getByLabel('Subject')).toHaveValue('The stock export fails');
  await minh.getByRole('button', { name: 'Create conversation' }).click();
  await expect(minh).toHaveCount(0);
  await expect(page.getByText("Minh's card opened a conversation.")).toBeVisible();

  // Set Lan's card aside, answered in chat.
  await lan.getByRole('button', { name: 'Decide' }).click();
  await lan.getByRole('radio', { name: 'Not a request' }).check();
  await lan.getByRole('radio', { name: 'Answered in chat' }).check();
  await lan.getByRole('button', { name: 'Set aside' }).click();
  await expect(page.getByText('Nothing waits for a person.', { exact: false })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Review' })).toBeVisible();

  // Decided: who decided each, the conversation Minh's card opened, and Put back for Lan's.
  await page.getByRole('tab', { name: 'Decided' }).click();
  const opened = page.getByRole('article', { name: 'Minh' });
  await expect(opened).toContainText('Opened a conversation');
  await expect(opened).toContainText(admin);
  // The inbox does not read conversations of the loopback stand-in type, so the link is checked, not followed.
  const href = await opened.getByRole('link', { name: 'Open the conversation' }).getAttribute('href');
  expect(href).toMatch(/^\/conversations\/[0-9a-f-]{36}$/);
  expect(await (await page.request.get(`${origin}/api/v1${href}`)).json()).toMatchObject({ subject: 'The stock export fails' });
  const setAside = page.getByRole('article', { name: 'Lan' });
  await expect(setAside).toContainText('Set aside: Answered in chat');
  await setAside.getByRole('button', { name: 'Put back in queue' }).click();
  await expect(page.getByText("Lan's card is back in Waiting.")).toBeVisible();
  await expect(setAside).toHaveCount(0);
  await page.getByRole('tab', { name: /Waiting/ }).click();
  await expect(page.getByRole('article', { name: 'Lan' })).toContainText('Put back by');
});
