import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { expect, test } from '@playwright/test';
import { createWorkspace, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import type { ChatServer } from './support/chat';
import { addLoopbackChannel, holdGroupsForReview, sendLoopback, startChatServer, waitForCards } from './support/chat';

// docs/user/review.md, AI on a chat channel at Suggest, on the real server and AI worker: a stand-in model
// suggests what to do with each card, a person follows it, and a thank-you after the conversation was
// resolved keeps it resolved.

// intakeModel stands in for an OpenAI-compatible provider. With no conversation offered it opens one; a
// thank-you joins the first conversation offered as an acknowledgement; anything else joins it.
async function intakeModel() {
  const server = createServer((request, response) => {
    let body = '';
    request.on('data', (chunk: Buffer) => { body += chunk.toString(); });
    request.on('end', () => {
      const chat = JSON.parse(body || '{}') as { messages?: Array<{ role: string; content: string }> };
      const prompt = chat.messages?.map((message) => message.content).join('\n') ?? '';
      const offered = /<open_conversations>\s*C1 /.test(prompt);
      const thanks = /Thank you/.test(prompt);
      const answer = !offered
        ? { action: 'create', target: '', subject: 'Stock export fails', command: '', confidence: 0.81, reason: 'A new problem with the export.' }
        : { action: 'attach', target: 'C1', subject: '', command: '', confidence: 0.93, reason: thanks ? 'She thanks the team for the fix.' : 'The same export problem.' };
      const content = JSON.stringify({ runner_up: '', runner_up_confidence: 0, ignore_reason: '', reply_kind: thanks ? 'acknowledgement' : 'substantive', ...answer });
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify({ id: 'fake', model: 'llama-4-scout', choices: [{ message: { content }, finish_reason: 'stop' }], usage: { prompt_tokens: 400, completion_tokens: 40 } }));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  return { url: `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1`, close: () => server.close() };
}

let chat: ChatServer;
let model: Awaited<ReturnType<typeof intakeModel>>;

test.beforeAll(async () => {
  model = await intakeModel();
  chat = await startChatServer({ AI_ALLOW_PRIVATE_BASE_URLS: '1' });
});
test.afterAll(async () => { await chat?.stop(); model?.close(); });

test('review with AI: a person follows what AI suggests, and a thank-you after it was resolved keeps it resolved', async ({ page }) => {
  test.setTimeout(120_000);
  const { origin } = chat;
  const headers = { Origin: origin };
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Review AI ${suffix}`, `admin-${suffix}@example.com`, origin, chat.databaseURL));
  const account = `ai-${suffix}`;
  const channel = await addLoopbackChannel(page.request, origin, account);
  await holdGroupsForReview(page.request, origin, channel);
  const provider = await page.request.post(`${origin}/api/v1/ai/providers`, { headers,
    data: { kind: 'openai_compatible', label: 'Office model', base_url: model.url, models: ['llama-4-scout'] } });
  expect(provider.status(), await provider.text()).toBe(201);
  const { id } = await provider.json() as { id: string };
  const slot = await page.request.put(`${origin}/api/v1/ai/slots/intake`, { headers, data: { source: 'provider', provider_id: id, enabled: true } });
  expect(slot.status(), await slot.text()).toBe(200);
  const level = await page.request.put(`${origin}/api/v1/channels/${channel}/ai/intake`, { headers, data: { scope: 'group', level: 'suggest' } });
  expect(level.status(), await level.text()).toBe(200);

  const suggested = async (text: string) => {
    await expect.poll(async () => {
      const cards = await (await page.request.get(`${origin}/api/v1/intake/gatherings?state=review&channel_id=${channel}`)).json() as { items: { preview: string; suggestion: unknown }[] };
      return cards.items.some((card) => card.preview.includes(text) && card.suggestion !== null);
    }, { timeout: 45_000 }).toBe(true);
  };
  const review = page.getByRole('article', { name: 'Minh' });

  // No conversation yet: AI suggests opening one, and Follow opens it.
  await sendLoopback(page.request, origin, account, [{ id: `a1-${suffix}`, from: 'a', name: 'Minh', group: 'g1', text: 'The stock export fails' }]);
  await waitForCards(page.request, origin, channel, 1);
  await suggested('The stock export fails');
  await page.goto(`${origin}/review`);
  const first = review.getByRole('group', { name: 'Suggested by AI' });
  await expect(first).toContainText('New conversation: “Stock export fails” · 81%');
  await expect(first).toContainText('A new problem with the export.');
  await first.getByRole('button', { name: 'Follow suggestion' }).click();
  await expect(page.getByText("Minh's card opened a conversation.")).toBeVisible();
  const conversations = await (await page.request.get(`${origin}/api/v1/conversations?page=1&page_size=50`)).json() as { items: { id: string; subject: string }[] };
  const conversation = conversations.items.find((item) => item.subject === 'Stock export fails')!.id;

  // The next message joins it when followed.
  await sendLoopback(page.request, origin, account, [{ id: `a2-${suffix}`, from: 'a', name: 'Minh', group: 'g1', text: 'It fails on every page since this morning' }]);
  await suggested('It fails on every page');
  await page.reload();
  const second = review.getByRole('group', { name: 'Suggested by AI' });
  await expect(second).toContainText('Add to “Stock export fails” · 93%');
  await second.getByRole('button', { name: 'Follow suggestion' }).click();
  await expect(page.getByText("Minh's card was added to a conversation.")).toBeVisible();

  // Resolved, then thanked: following keeps it resolved, and the conversation says so with Reopen.
  expect((await page.request.patch(`${origin}/api/v1/conversations/${conversation}`, { headers, data: { status: 'resolved' } })).status()).toBe(200);
  await sendLoopback(page.request, origin, account, [{ id: `a3-${suffix}`, from: 'a', name: 'Minh', group: 'g1', text: 'Thank you, it works now' }]);
  await suggested('Thank you');
  await page.reload();
  const third = review.getByRole('group', { name: 'Suggested by AI' });
  await expect(third).toContainText('Resolved: a thank-you, so it stays resolved');
  await third.getByRole('button', { name: 'Follow suggestion' }).click();
  await expect(page.getByText("Minh's card was added to a conversation.")).toBeVisible();
  expect(await (await page.request.get(`${origin}/api/v1/conversations/${conversation}`)).json()).toMatchObject({ status: 'resolved' });
  await page.goto(`${origin}/conversations/${conversation}`);
  const thanks = page.getByRole('region', { name: 'Timeline' }).getByRole('article').filter({ hasText: 'Thank you, it works now' });
  await expect(thanks).toContainText('Written after it was resolved · kept Resolved');
  await thanks.getByRole('button', { name: 'Reopen' }).click();
  await expect.poll(async () => ((await (await page.request.get(`${origin}/api/v1/conversations/${conversation}`)).json()) as { status: string }).status).toBe('open');
});
