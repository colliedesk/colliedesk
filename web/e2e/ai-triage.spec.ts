import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { expect, test } from '@playwright/test';
import { psql, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

const baseURL = servers.main;
const headers = { Origin: baseURL };

// triageModel stands in for an OpenAI-compatible provider: it suggests the tag "hardware", a tag the
// team does not have, urgent, and Sam.
async function triageModel() {
  const prompts: string[] = [];
  const server = createServer((request, response) => {
    let body = '';
    request.on('data', (chunk: Buffer) => { body += chunk.toString(); });
    request.on('end', () => {
      const chat = JSON.parse(body || '{}') as { messages?: Array<{ role: string; content: string }> };
      prompts.push(chat.messages?.map((message) => message.content).join('\n') ?? '');
      const content = JSON.stringify({ tags: ['hardware', 'made-up'], priority: 'urgent', assignee: 'Sam Rivera', reason: 'The whole office cannot print.' });
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify({ id: 'fake', model: 'llama-4-scout', choices: [{ message: { content }, finish_reason: 'stop' }], usage: { prompt_tokens: 300, completion_tokens: 30 } }));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  return { url: `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1`, prompts, close: () => server.close() };
}

test('a new ticket by email gets an AI triage suggestion that changes nothing until someone accepts a part of it', async ({ page }) => {
  const suffix = uniqueSuffix();
  const support = `support-${suffix}@acme.example`;
  const workspace = createWorkspaceWithSlug(`E2E Triage ${suffix}`, `admin-${suffix}@acme.example`);
  const model = await triageModel();
  try {
    await redeemLink(page, workspace.url);
    expect((await page.request.put(`${baseURL}/api/v1/channels/email`, { headers, data: { support_address: support } })).status()).toBe(200);
    const { inbound_secret: secret } = await (await page.request.post(`${baseURL}/api/v1/channels/email/secret`, { headers })).json() as { inbound_secret: string };
    expect((await page.request.post(`${baseURL}/api/v1/tags`, { headers, data: { name: 'hardware' } })).status()).toBe(201);
    const me = await (await page.request.get(`${baseURL}/api/v1/auth/me`)).json() as { agent: { id: string } };
    expect((await page.request.patch(`${baseURL}/api/v1/agents/${me.agent.id}`, { headers, data: { display_name: 'Sam Rivera' } })).status()).toBe(200);
    const provider = await page.request.post(`${baseURL}/api/v1/ai/providers`, { headers,
      data: { kind: 'openai_compatible', label: 'Office model', base_url: model.url, models: ['llama-4-scout'] } });
    expect(provider.status(), await provider.text()).toBe(201);
    const { id } = await provider.json() as { id: string };
    expect((await page.request.put(`${baseURL}/api/v1/ai/slots/triage`, { headers, data: { source: 'provider', provider_id: id, enabled: true } })).status()).toBe(200);

    const inbound = await page.request.post(`${baseURL}/api/v1/channels/email/inbound`, {
      headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret },
      data: Buffer.from([`From: Erin Customer <erin-${suffix}@customer.example>`, `To: ${support}`, `Subject: Printer down ${suffix}`,
        `Message-ID: <triage-${suffix}@customer.example>`, 'Date: Thu, 22 Oct 2026 09:00:00 +0000', 'MIME-Version: 1.0',
        'Content-Type: text/plain; charset=utf-8', '', 'The office printer is down for everyone.', ''].join('\r\n')),
    });
    expect(inbound.status()).toBe(200);
    const { conversation_id: conversation } = await inbound.json() as { conversation_id: string };

    await page.goto(`${baseURL}/conversations/${conversation}`);
    const card = page.getByRole('region', { name: 'Suggested by AI' });
    await expect(card.getByRole('button', { name: 'Accept tags hardware' })).toBeVisible({ timeout: 30_000 });
    await expect(card.getByRole('group', { name: 'Priority' })).toContainText('Urgent');
    await expect(card.getByRole('group', { name: 'Assignee' })).toContainText('Sam Rivera');
    await expect(card.getByText('made-up')).toHaveCount(0);
    expect(model.prompts.at(-1)).toContain('The office printer is down for everyone.');
    // Nothing applied yet.
    let shown = await (await page.request.get(`${baseURL}/api/v1/conversations/${conversation}`)).json() as { priority: string; assignee_id: string | null; tags: unknown[] };
    expect(shown).toMatchObject({ priority: 'normal', assignee_id: null, tags: [] });

    // One part, then the rest but the assignee: dismissing it leaves the suggestion partly accepted.
    await card.getByRole('button', { name: 'Accept priority Urgent' }).click();
    await expect(card.getByRole('group', { name: 'Priority' })).toContainText('Set');
    await expect(page.getByRole('combobox', { name: 'Priority' })).toHaveValue('urgent');
    await card.getByRole('button', { name: 'Accept tags hardware' }).click();
    await expect(card.getByRole('group', { name: 'Tags' })).toContainText('Added');
    await card.getByRole('button', { name: 'Dismiss' }).click();
    await expect(card.getByText('Part of it was applied; the rest was dismissed.')).toBeVisible();
    shown = await (await page.request.get(`${baseURL}/api/v1/conversations/${conversation}`)).json() as typeof shown;
    expect(shown.priority).toBe('urgent');
    expect(shown.assignee_id).toBeNull();
    expect(shown.tags).toHaveLength(1);
    expect(psql(`SELECT outcome FROM ai_triage_suggestions s JOIN workspaces w ON w.id=s.workspace_id WHERE w.slug='${workspace.slug}'`)).toBe('partly_accepted');

    // Asked again by hand, on a phone: the details hide behind a toggle that says a suggestion waits, and
    // the suggestion's buttons are on screen once it opens.
    await card.getByRole('button', { name: 'Suggest again' }).click();
    await expect(card.getByRole('button', { name: 'Accept assignee Sam Rivera' })).toBeVisible({ timeout: 30_000 });
    await page.setViewportSize({ width: 390, height: 844 });
    const toggle = page.getByRole('button', { name: /Show details.*AI suggestion/ });
    await expect(toggle).toBeVisible();
    await toggle.click();
    await expect(card.getByRole('button', { name: 'Accept all' })).toBeInViewport();
    await card.getByRole('button', { name: 'Accept all' }).click();
    await expect(card.getByText(/Accepted by you, .* Change a field below to undo\./)).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  } finally {
    model.close();
  }
});
