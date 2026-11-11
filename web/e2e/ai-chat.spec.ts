import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { expect, test } from '@playwright/test';
import type { APIRequestContext, Page } from '@playwright/test';
import { psql, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

const baseURL = servers.main;
const headers = { Origin: baseURL };

interface ChatRequest { stream?: boolean; messages?: Array<{ role: string; content: unknown }> }

// chatModel stands in for an OpenAI-compatible provider: it answers a streamed chat in pieces, citing
// the first article the prompt offered, and answers the provider check without streaming.
async function chatModel() {
  const server = createServer((request, response) => {
    let body = '';
    request.on('data', (chunk: Buffer) => { body += chunk.toString(); });
    request.on('end', () => {
      const chat = JSON.parse(body || '{}') as ChatRequest;
      if (!chat.stream) {
        response.setHeader('Content-Type', 'application/json');
        response.end(JSON.stringify({ id: 'fake', model: 'chat-model', choices: [{ message: { content: 'ok' }, finish_reason: 'stop' }], usage: { prompt_tokens: 5, completion_tokens: 1 } }));
        return;
      }
      const number = /number=\\?"(\d+)\\?"/.exec(JSON.stringify(chat.messages ?? []))?.[1] ?? '0';
      response.setHeader('Content-Type', 'text/event-stream');
      const pieces = ['Orders ship ', `in 2 days [#${number}].`, ` Pack them before noon [#${number}].`];
      for (const text of pieces) response.write(`data: ${JSON.stringify({ id: 'fake', choices: [{ index: 0, delta: { content: text }, finish_reason: null }] })}\n\n`);
      response.write(`data: ${JSON.stringify({ id: 'fake', choices: [{ index: 0, delta: {}, finish_reason: 'stop' }] })}\n\n`);
      response.write(`data: ${JSON.stringify({ id: 'fake', choices: [], usage: { prompt_tokens: 900, completion_tokens: 30 } })}\n\n`);
      response.end('data: [DONE]\n\n');
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  return { url: `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1`, close: () => server.close() };
}

// aSite is a company's own site on another origin; show puts the chat's widget code on its page.
async function aSite() {
  let page = '<!doctype html><title>Shop</title><h1>Shop</h1>';
  const server = createServer((_request, response) => {
    response.setHeader('Content-Type', 'text/html; charset=utf-8');
    response.end(page);
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  return {
    origin: `http://127.0.0.1:${(server.address() as AddressInfo).port}`,
    show: (script: string, channel: string) => { page += `<script src="${script}" data-channel="${channel}" async></script>`; },
    close: () => server.close(),
  };
}

interface Desk { slug: string; help: string; channel: string }

// aDeskWithChat signs in a new workspace's admin and turns on its help center with one published
// article, the AI chat on the stand-in model, and a help chat channel.
async function aDeskWithChat(page: Page, name: string, model: string, widgetOrigins: string[] = []): Promise<Desk> {
  const suffix = uniqueSuffix();
  const workspace = createWorkspaceWithSlug(`${name} ${suffix}`, `admin-${suffix}@chat.example`);
  await redeemLink(page, workspace.url);
  const call = async (request: APIRequestContext, method: 'post' | 'put', path: string, data: unknown, status: number) => {
    const response = await request[method](`${baseURL}${path}`, { headers, data });
    expect(response.status(), `${method} ${path}: ${await response.text()}`).toBe(status);
    return response.json() as Promise<{ id: string }>;
  };
  await call(page.request, 'put', '/api/v1/help-center', { enabled: true, title: `${name} Help`, locale: 'en' }, 200);
  const collection = await call(page.request, 'post', '/api/v1/kb/collections', { name: 'Orders' }, 201);
  await call(page.request, 'post', '/api/v1/kb/articles', { collection_id: collection.id, title: 'Shipping times', status: 'published',
    body_md: 'Orders ship in 2 days. Pack them before noon.' }, 201);
  const provider = await call(page.request, 'post', '/api/v1/ai/providers', { kind: 'openai_compatible', label: 'Chat model', base_url: model, models: ['chat-model'] }, 201);
  await call(page.request, 'put', '/api/v1/ai/slots/chat', { source: 'provider', provider_id: provider.id, enabled: true }, 200);
  const channel = await call(page.request, 'post', '/api/v1/channels', { type: 'help_chat',
    config: { greeting: 'Hi! Ask about your orders.', suggestions: ['When does my order ship?'], widget_origins: widgetOrigins } }, 201);
  return { slug: workspace.slug, help: `${baseURL}/help/${workspace.slug}`, channel: channel.id };
}

test('a customer asks the help chat, reads a cited answer, comes back to it, hands it to the team, starts over, and is paused at the limit', async ({ page }) => {
  test.setTimeout(240_000);
  const model = await chatModel();
  try {
    const desk = await aDeskWithChat(page, 'E2E Chat', model.url);
    await page.goto(`${desk.help}/chat`);
    await expect(page.getByText('Hi! Ask about your orders.')).toBeVisible();
    await page.getByRole('button', { name: 'When does my order ship?' }).click();
    const answer = page.locator('.turn').last();
    await expect(answer.locator('.a-body')).toContainText('Orders ship in 2 days.');
    await expect(answer.locator('.sources a.tab')).toHaveAttribute('title', 'Shipping times');
    await expect(answer.locator('a.cite').first()).toHaveAttribute('aria-label', 'Source 1: Shipping times');

    // The chat is the browser's: a reload shows it again.
    await page.reload();
    await expect(page.locator('.turn .q')).toContainText('When does my order ship?');

    // Talk to a person: the team gets a conversation on the help chat channel with the whole chat.
    await page.getByRole('button', { name: 'Talk to a person' }).first().click();
    await page.getByLabel('Email').fill('reader@customer.example');
    await page.getByRole('button', { name: /^Send to / }).click();
    const sent = page.locator('.note-ok');
    await expect(sent).toContainText('Reference #');
    const conversations = await (await page.request.get(`${baseURL}/api/v1/conversations`)).json() as { items: Array<{ id: string; channel: string; subject: string }> };
    const handedOff = conversations.items.find((item) => item.channel === 'help_chat');
    expect(handedOff?.subject).toBe('Help chat: When does my order ship?');
    await expect(sent).toContainText(handedOff!.id.replaceAll('-', '').slice(0, 8).toUpperCase());
    const timeline = await (await page.request.get(`${baseURL}/api/v1/conversations/${handedOff!.id}/messages`)).json() as { items: Array<{ body: string }> };
    expect(timeline.items[0].body).toContain('Customer: When does my order ship?');
    // The team reads the chat it came from.
    await page.goto(`${baseURL}/settings/ai/conversations/${handedOff!.id}/chat`);
    await expect(page.getByText('Orders ship in 2 days [1]. Pack them before noon [1].', { exact: true })).toBeVisible();

    // New chat clears it from this browser.
    await page.goto(`${desk.help}/chat`);
    await page.getByRole('button', { name: 'New chat' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Start new chat' }).click();
    await expect(page.getByRole('heading', { name: 'How can we help?' })).toBeVisible();
    await expect(page.locator('.turn')).toHaveCount(0);

    // Past the month's limit, the chat offers the matching articles and a person instead of an answer.
    const workspace = psql(`SELECT id FROM workspaces WHERE slug='${desk.slug}'`);
    // The matching articles come from the AI search index, which a background job builds every few seconds
    // (an answer reads the help center itself); on a busy server it can still be behind the new article.
    await expect.poll(() => psql(`SELECT count(*) FROM ai_retrieval_docs WHERE workspace_id='${workspace}' AND kb_article_id IS NOT NULL`),
      { timeout: 30_000 }).toBe('1');
    psql(`UPDATE ai_slots SET monthly_cap_micro_usd=1 WHERE workspace_id='${workspace}' AND slot='chat'`);
    psql(`INSERT INTO ai_usage(workspace_id,feature,billed_to,provider_kind,provider_label,model,status,cost_micro_usd,started_at,finished_at)
      VALUES('${workspace}','chat','workspace_key','openai_compatible','Chat model','chat-model','ok',5,now(),now())`);
    await page.getByLabel('Your question').fill('Can I pack after noon?');
    await page.getByLabel('Your question').press('Enter');
    await expect(page.getByText('AI answers are paused for now.')).toBeVisible();
    await expect(page.locator('.note .index').getByRole('link', { name: /Shipping times/ })).toBeVisible();
  } finally {
    model.close();
  }
});

test('the chat shows on a site the channel lists, and a chat cookie of one workspace reads nothing of another', async ({ page, browser }) => {
  test.setTimeout(240_000);
  const model = await chatModel();
  const listed = await aSite();
  const unlisted = await aSite();
  const visitor = await browser.newPage();
  try {
    const desk = await aDeskWithChat(page, 'E2E Widget', model.url, [listed.origin]);
    listed.show(`${desk.help}/chat/widget.js`, desk.channel);
    unlisted.show(`${desk.help}/chat/widget.js`, desk.channel);
    await visitor.goto(listed.origin);
    // The button lives in a closed shadow root: press it where it is drawn.
    await expect.poll(() => visitor.evaluate(() => Boolean(document.querySelector('[data-colliedesk-chat-root]')))).toBe(true);
    const size = visitor.viewportSize()!;
    await visitor.mouse.click(size.width - 48, size.height - 48);
    await expect.poll(() => visitor.frames().some((frame) => frame.url().includes('/chat?embed=1'))).toBe(true);
    const frame = visitor.frames().find((item) => item.url().includes('/chat?embed=1'))!;
    await frame.getByLabel('Your question').fill('When does my order ship?');
    await frame.getByLabel('Your question').press('Enter');
    await expect(frame.locator('.turn .a-body')).toContainText('Orders ship in 2 days.');
    // A site the channel does not list draws nothing.
    await visitor.goto(unlisted.origin);
    await visitor.waitForLoadState('networkidle');
    expect(await visitor.evaluate(() => Boolean(document.querySelector('[data-colliedesk-chat-root]')))).toBe(false);

    // Workspace A's chat cookie, sent to workspace B's help center under B's own cookie name, finds no chat.
    await page.goto(`${desk.help}/chat`);
    await page.getByRole('button', { name: 'When does my order ship?' }).click();
    await expect(page.locator('.turn .a-body').last()).toContainText('Orders ship in 2 days.');
    const cookie = (await page.context().cookies()).find((item) => item.name.startsWith(`cd_chat_${desk.slug}_`));
    expect(cookie).toBeTruthy();
    const other = await aDeskWithChat(page, 'E2E Other', model.url);
    const borrowed = `cd_chat_${other.slug}_${other.channel.slice(0, 8)}=${cookie!.value}`;
    const read = await page.request.get(`${other.help}/chat/session`, { headers: { Cookie: borrowed } });
    expect(read.status()).toBe(204);
  } finally {
    await visitor.close();
    listed.close();
    unlisted.close();
    model.close();
  }
});
