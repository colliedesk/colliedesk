import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { expect, test } from '@playwright/test';
import { psql, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

const baseURL = servers.main;
const headers = { Origin: baseURL };

// draftModel stands in for an OpenAI-compatible provider that drafts a short reply.
async function draftModel() {
  let calls = 0;
  const server = createServer((request, response) => {
    request.resume();
    request.on('end', () => {
      calls += 1;
      const content = JSON.stringify({ reply_text: 'Hi Erin, the office printer is back: tray two was jammed.', source_ids: [], missing_info: '', reply_needed: true });
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify({ id: 'fake', model: 'llama-4-scout', choices: [{ message: { content }, finish_reason: 'stop' }], usage: { prompt_tokens: 900, completion_tokens: 40 } }));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  return { url: `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1`, calls: () => calls, close: () => server.close() };
}

test('an admin lets drafts write themselves; a customer email gets a draft the team uses and sends, and AI sends nothing', async ({ page }) => {
  // Three drafts wait for an AI worker tick (up to 5 s each) and two of them for the card's next check
  // (every 10 s while a draft is being written): up to 35 s of waiting by design, past the default 30 s.
  test.setTimeout(60_000);
  const suffix = uniqueSuffix();
  const support = `support-${suffix}@acme.example`;
  const workspace = createWorkspaceWithSlug(`E2E Auto Drafts ${suffix}`, `admin-${suffix}@acme.example`);
  const model = await draftModel();
  try {
    await redeemLink(page, workspace.url);
    expect((await page.request.put(`${baseURL}/api/v1/channels/email`, { headers, data: { support_address: support } })).status()).toBe(200);
    const { inbound_secret: secret } = await (await page.request.post(`${baseURL}/api/v1/channels/email/secret`, { headers })).json() as { inbound_secret: string };
    const provider = await page.request.post(`${baseURL}/api/v1/ai/providers`, { headers,
      data: { kind: 'openai_compatible', label: 'Office model', base_url: model.url, models: ['llama-4-scout'] } });
    expect(provider.status(), await provider.text()).toBe(201);
    const { id } = await provider.json() as { id: string };
    expect((await page.request.put(`${baseURL}/api/v1/ai/slots/draft`, { headers, data: { source: 'provider', provider_id: id, enabled: true } })).status()).toBe(200);

    // Settings → Built-in AI: the switch asks first, with what it costs on the team's own key.
    await page.goto(`${baseURL}/settings/ai`);
    await page.getByRole('switch', { name: 'Write a draft when a customer writes' }).click();
    const dialog = page.getByRole('dialog', { name: 'Write a draft for every customer message?' });
    await expect(dialog.getByText(/Billed to your key, Office model/)).toBeVisible();
    await dialog.getByRole('button', { name: 'Turn on' }).click();
    await expect(page.getByRole('switch', { name: 'Write a draft when a customer writes' })).toBeChecked();

    // Adding the provider checked it with one call; from here, one call is one draft.
    const before = model.calls();
    const inbound = await page.request.post(`${baseURL}/api/v1/channels/email/inbound`, {
      headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret },
      data: Buffer.from([`From: Erin Customer <erin-${suffix}@customer.example>`, `To: ${support}`, `Subject: Printer down ${suffix}`,
        `Message-ID: <auto-draft-${suffix}@customer.example>`, 'Date: Thu, 22 Oct 2026 09:00:00 +0000', 'MIME-Version: 1.0',
        'Content-Type: text/plain; charset=utf-8', '', 'The office printer is down for everyone.', ''].join('\r\n')),
    });
    expect(inbound.status()).toBe(200);
    const { conversation_id: conversation } = await inbound.json() as { conversation_id: string };
    // It waits a minute for more messages from the customer; the test does not.
    psql(`UPDATE ai_tasks SET next_run_at=now() WHERE conversation_id='${conversation}' AND kind='draft' AND automatic AND state='queued'`);

    await page.goto(`${baseURL}/conversations/${conversation}`);
    const card = page.getByRole('region', { name: 'Automatic AI draft' });
    // On this 720-pixel screen it starts folded, with its buttons; Show draft opens it.
    await card.getByRole('button', { name: 'Show draft' }).click({ timeout: 30_000 });
    await expect(card).toContainText('Hi Erin, the office printer is back');
    expect(psql(`SELECT count(*) FROM messages WHERE conversation_id='${conversation}' AND kind='outgoing'`)).toBe('0');
    await card.getByRole('button', { name: 'Use draft' }).click();
    await expect(page.getByRole('textbox', { name: 'Message body' })).toHaveValue(/Hi Erin, the office printer is back/);
    await page.getByRole('button', { name: 'Send reply' }).click();
    await expect(page.getByRole('article').filter({ hasText: 'Hi Erin, the office printer is back' })).toBeVisible();
    expect(psql(`SELECT outcome FROM ai_drafts WHERE conversation_id='${conversation}' AND automatic`)).toMatch(/^sent_/);
    expect(model.calls() - before).toBe(1);

    // The customer writes twice more; the second time the conversation had its 5 automatic drafts in a
    // day. The draft for the first stays usable, the limit is a line in its card, and on a short laptop
    // screen the draft folds to a line so the conversation keeps the column.
    const customerWrites = (body: string) => psql(`INSERT INTO messages(workspace_id,conversation_id,kind,body)
      SELECT workspace_id,id,'incoming','${body}' FROM conversations WHERE id='${conversation}'`);
    customerWrites('Thanks, and the scanner?');
    psql(`UPDATE ai_tasks SET next_run_at=now() WHERE conversation_id='${conversation}' AND kind='draft' AND automatic AND state='queued'`);
    await page.reload();
    await expect(card.getByRole('button', { name: 'Use draft' })).toBeVisible({ timeout: 30_000 });
    psql(`INSERT INTO ai_tasks(workspace_id,kind,conversation_id,automatic,state,started_at,finished_at)
      SELECT workspace_id,'draft',id,true,'done',now()-g*interval '1 hour',now() FROM conversations, generate_series(1,4) g WHERE id='${conversation}'`);
    customerWrites('Hello? Anyone there for the scanner too?');
    psql(`UPDATE ai_tasks SET next_run_at=now() WHERE conversation_id='${conversation}' AND kind='draft' AND automatic AND state='queued'`);
    await expect.poll(() => psql(`SELECT count(*) FROM ai_tasks WHERE conversation_id='${conversation}' AND error_code='conversation_cap'`), { timeout: 30_000 }).toBe('1');
    await page.setViewportSize({ width: 1280, height: 720 });
    await page.reload();
    await expect(card.getByText('No automatic draft for this message:')).toBeVisible();
    // Folded on a short screen: one line with its buttons, the rest of the column is the conversation.
    await expect(card.getByRole('button', { name: 'Use draft' })).toBeInViewport();
    expect((await card.boundingBox())!.height).toBeLessThan(140);
    await card.getByRole('button', { name: 'Show draft' }).click();
    await expect(card).toContainText('Hi Erin, the office printer is back');
    await card.getByRole('button', { name: 'Hide' }).click();
    expect(model.calls() - before).toBe(2);

    // A phone: the card's buttons are in reach and nothing scrolls sideways.
    await page.setViewportSize({ width: 390, height: 844 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  } finally {
    model.close();
  }
});
