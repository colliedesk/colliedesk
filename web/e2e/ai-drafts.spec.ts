import { mkdtempSync } from 'node:fs';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { AddressInfo } from 'node:net';
import { expect, test } from '@playwright/test';
import { colliedeskAsync, psql, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { FakeFreshdesk } from './support/fake-freshdesk';
import { createWorkspaceWithSlug } from './support/workspace';

const baseURL = servers.main;
const headers = { Origin: baseURL };
const apiKey = 'fd-e2e-drafts-key';

interface ChatRequest { messages: Array<{ role: string; content: string }> }

// draftModel stands in for an OpenAI-compatible provider: it drafts a reply that cites every source the
// prompt offered, and keeps the prompts it was sent.
async function draftModel() {
  const prompts: string[] = [];
  const server = createServer((request, response) => {
    let body = '';
    request.on('data', (chunk: Buffer) => { body += chunk.toString(); });
    request.on('end', () => {
      const chat = JSON.parse(body || '{}') as ChatRequest;
      const user = chat.messages?.filter((message) => message.role === 'user').map((message) => message.content).join('\n') ?? '';
      prompts.push(user);
      const sources = [...user.matchAll(/id="((?:kb|past)\d+)"/g)].map((match) => match[1]);
      const content = JSON.stringify({ reply_text: 'Hello, your parcel left Rotterdam this morning and arrives tomorrow.', source_ids: sources, missing_info: 'The tracking number.' });
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify({ id: 'fake', model: 'llama-4-scout', choices: [{ message: { content }, finish_reason: 'stop' }], usage: { prompt_tokens: 900, completion_tokens: 40 } }));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  return { url: `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1`, prompts, close: () => server.close() };
}

test('a team that imported Freshdesk drafts a reply with AI from its own solved tickets, edits it and sends it', async ({ page }) => {
  test.setTimeout(240_000);
  const suffix = uniqueSuffix();
  const adminEmail = `alice-${suffix}@acme.example`;
  const workspace = createWorkspaceWithSlug(`E2E Drafts ${suffix}`, adminEmail);
  const fake = new FakeFreshdesk({ apiKey, agentEmail: adminEmail });
  const model = await draftModel();
  await fake.start();
  try {
    await redeemLink(page, workspace.url);
    const imported = await colliedeskAsync(['import', 'freshdesk', '--workspace', workspace.slug, '--domain', 'acme.freshdesk.com', '--report-dir', mkdtempSync(join(tmpdir(), 'colliedesk-drafts-report-'))],
      { tags: 'e2e', env: { ...fake.env(), FRESHDESK_API_KEY: apiKey } });
    expect(imported.status, imported.stderr).toBe(0);
    // An import queues no AI work: drafts are asked for by people.
    expect(psql(`SELECT count(*) FROM ai_tasks t JOIN workspaces w ON w.id=t.workspace_id WHERE w.slug='${workspace.slug}'`)).toBe('0');

    const provider = await page.request.post(`${baseURL}/api/v1/ai/providers`, { headers,
      data: { kind: 'openai_compatible', label: 'Office model', base_url: model.url, models: ['llama-4-scout'] } });
    expect(provider.status()).toBe(201);
    const { id } = await provider.json() as { id: string };
    expect((await page.request.put(`${baseURL}/api/v1/ai/slots/draft`, { headers, data: { source: 'provider', provider_id: id, enabled: true } })).status()).toBe(200);

    await page.reload();
    await page.getByRole('searchbox', { name: 'Search conversations' }).fill('Rotterdam');
    await page.getByRole('button', { name: /Fixture ticket 42:/ }).click();
    // On a phone the panel's button is on screen without scrolling.
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: /^Draft with AI/ }).click();
    const panel = page.getByRole('region', { name: 'Draft with AI' });
    await expect(panel.getByRole('button', { name: 'Write draft' })).toBeInViewport();
    await page.setViewportSize({ width: 1280, height: 720 });
    await panel.getByLabel('What should the reply say? (optional)').fill('Tell them where the parcel is');
    await panel.getByRole('button', { name: 'Write draft' }).click();
    // The sources are built in the background after an import; the draft waits for nothing on screen.
    await expect(panel.getByText('Hello, your parcel left Rotterdam this morning and arrives tomorrow.')).toBeVisible({ timeout: 30_000 });
    await expect(panel.getByText(/The tracking number\./)).toBeVisible();
    await expect(panel.getByRole('link').first()).toBeVisible();
    // On a short laptop screen the draft's buttons and Send are on screen without scrolling.
    await expect(panel.getByRole('button', { name: 'Use draft' })).toBeInViewport();
    await expect(page.getByRole('button', { name: 'Send reply' })).toBeInViewport();

    const prompt = model.prompts.at(-1) ?? '';
    expect(prompt).toContain('Tell them where the parcel is');
    expect(prompt).toContain('Customer follow-up on 42');
    expect(prompt).not.toContain(apiKey);

    await panel.getByRole('button', { name: 'Use draft' }).click();
    const box = page.getByLabel('Message body').last();
    await expect(box).toHaveValue('Hello, your parcel left Rotterdam this morning and arrives tomorrow.');
    await box.fill('Hello, your parcel left Rotterdam this morning and arrives tomorrow by noon.');
    await page.getByRole('button', { name: 'Send reply' }).click();
    await expect(page.getByText('Hello, your parcel left Rotterdam this morning and arrives tomorrow by noon.')).toBeVisible();
    await expect(page.getByText('from an AI draft')).toBeVisible();
    expect(psql(`SELECT outcome FROM ai_drafts d JOIN workspaces w ON w.id=d.workspace_id WHERE w.slug='${workspace.slug}'`)).toBe('sent_edited');
  } finally {
    model.close();
    await fake.stop();
  }
});
