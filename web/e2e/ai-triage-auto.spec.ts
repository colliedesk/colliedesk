import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { expect, test } from '@playwright/test';
import { psql, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

const baseURL = servers.main;
const headers = { Origin: baseURL };

// triageModel stands in for an OpenAI-compatible provider: it suggests the tag "hardware" and urgent.
async function triageModel() {
  const server = createServer((request, response) => {
    request.resume();
    request.on('end', () => {
      const content = JSON.stringify({ tags: ['hardware'], priority: 'urgent', assignee: '', reason: 'The whole office cannot print.', source_ids: [] });
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify({ id: 'fake', model: 'llama-4-scout', choices: [{ message: { content }, finish_reason: 'stop' }], usage: { prompt_tokens: 300, completion_tokens: 30 } }));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  return { url: `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1`, close: () => server.close() };
}

test('an admin lets triage apply by itself; a new ticket gets its tag and priority with "Applied by AI", and Undo takes them back', async ({ page }) => {
  const suffix = uniqueSuffix();
  const support = `support-${suffix}@acme.example`;
  const workspace = createWorkspaceWithSlug(`E2E Auto Triage ${suffix}`, `admin-${suffix}@acme.example`);
  const model = await triageModel();
  try {
    await redeemLink(page, workspace.url);
    expect((await page.request.put(`${baseURL}/api/v1/channels/email`, { headers, data: { support_address: support } })).status()).toBe(200);
    const { inbound_secret: secret } = await (await page.request.post(`${baseURL}/api/v1/channels/email/secret`, { headers })).json() as { inbound_secret: string };
    expect((await page.request.post(`${baseURL}/api/v1/tags`, { headers, data: { name: 'hardware' } })).status()).toBe(201);
    const provider = await page.request.post(`${baseURL}/api/v1/ai/providers`, { headers,
      data: { kind: 'openai_compatible', label: 'Office model', base_url: model.url, models: ['llama-4-scout'] } });
    expect(provider.status(), await provider.text()).toBe(201);
    const { id } = await provider.json() as { id: string };
    expect((await page.request.put(`${baseURL}/api/v1/ai/slots/triage`, { headers, data: { source: 'provider', provider_id: id, enabled: true } })).status()).toBe(200);

    // Settings → Built-in AI: raising triage's default for channels to Auto asks first, and says what AI will do.
    await page.goto(`${baseURL}/settings/ai`);
    const triage = page.getByRole('group', { name: 'Triage: default for channels' });
    await triage.getByRole('radio', { name: 'Auto' }).click();
    const dialog = page.getByRole('dialog', { name: 'Apply triage suggestions automatically?' });
    await expect(dialog.getByText(/Only fills what nobody set/)).toBeVisible();
    await dialog.getByRole('button', { name: 'Turn on' }).click();
    await expect(triage.getByRole('radio', { name: 'Auto' })).toBeChecked();

    const inbound = await page.request.post(`${baseURL}/api/v1/channels/email/inbound`, {
      headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret },
      data: Buffer.from([`From: Erin Customer <erin-${suffix}@customer.example>`, `To: ${support}`, `Subject: Printer down ${suffix}`,
        `Message-ID: <auto-${suffix}@customer.example>`, 'Date: Thu, 22 Oct 2026 09:00:00 +0000', 'MIME-Version: 1.0',
        'Content-Type: text/plain; charset=utf-8', '', 'The office printer is down for everyone.', ''].join('\r\n')),
    });
    expect(inbound.status()).toBe(200);
    const { conversation_id: conversation } = await inbound.json() as { conversation_id: string };

    await page.goto(`${baseURL}/conversations/${conversation}`);
    const line = page.getByRole('group', { name: 'Applied by AI' });
    await expect(line).toContainText('hardware', { timeout: 30_000 });
    await expect(page.getByRole('combobox', { name: 'Priority' })).toHaveValue('urgent');
    await line.getByRole('button', { name: /^Undo/ }).click();
    await expect(page.getByRole('group', { name: 'Undone' })).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Priority' })).toHaveValue('normal');
    expect(psql(`SELECT outcome FROM ai_triage_suggestions s JOIN workspaces w ON w.id=s.workspace_id WHERE w.slug='${workspace.slug}'`)).toBe('undone');

    // A phone: what AI did and Apply again are on the timeline, not behind the details toggle.
    await page.setViewportSize({ width: 390, height: 844 });
    const card = page.getByRole('region', { name: 'Suggested by AI' });
    await card.scrollIntoViewIfNeeded();
    await expect(card.getByRole('button', { name: 'Apply again' })).toBeInViewport();
    await card.getByRole('button', { name: 'Apply again' }).click();
    // Applied again it is AI's again, with its Undo; nothing becomes a one-way door.
    await expect(card.getByRole('heading', { name: /Applied by AI/ })).toBeVisible();
    await expect(card.getByRole('button', { name: 'Undo all' })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  } finally {
    model.close();
  }
});
