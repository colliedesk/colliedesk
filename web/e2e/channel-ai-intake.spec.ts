import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { expect, test } from '@playwright/test';
import { createWorkspace, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import type { ChatServer } from './support/chat';
import { addLoopbackChannel, startChatServer } from './support/chat';

// docs/user/ai.md "What AI does by itself, per channel" and "When AI acts on its own", on the real server:
// an admin turns chat intake on, sets it to Suggest on a chat channel's groups with settings of its own,
// and raising it to Auto asks first with what AI is ready for, saving an override with its reason.
let chat: ChatServer;
// A stand-in OpenAI-compatible provider: it answers the check when the provider is added; nothing here asks
// it to decide a card.
const model = createServer((request, response) => {
  request.resume();
  request.on('end', () => {
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify({ id: 'fake', model: 'llama-4-scout', choices: [{ message: { content: 'ok' }, finish_reason: 'stop' }], usage: { prompt_tokens: 1, completion_tokens: 1 } }));
  });
});

test.beforeAll(async () => {
  await new Promise<void>((resolve) => model.listen(0, '127.0.0.1', resolve));
  chat = await startChatServer({ AI_ALLOW_PRIVATE_BASE_URLS: '1' });
});
test.afterAll(async () => { await chat?.stop(); model.close(); });

test('intake on a chat channel: Suggest with settings of its own, then Auto asks what AI is ready for and keeps an override', async ({ page }) => {
  const { origin } = chat;
  const headers = { Origin: origin };
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Intake AI ${suffix}`, `admin-${suffix}@example.com`, origin, chat.databaseURL));
  const channel = await addLoopbackChannel(page.request, origin, `intake-${suffix}`);
  const provider = await page.request.post(`${origin}/api/v1/ai/providers`, { headers,
    data: { kind: 'openai_compatible', label: 'Office model', base_url: `http://127.0.0.1:${(model.address() as AddressInfo).port}/v1`, models: ['llama-4-scout'] } });
  expect(provider.status(), await provider.text()).toBe(201);

  // Built-in AI: intake has its row now that a chat channel collects messages; turn it on.
  await page.goto(`${origin}/settings/ai`);
  await page.getByRole('combobox', { name: 'Chat intake provider' }).selectOption({ label: 'Office model' });
  await page.getByRole('switch', { name: 'Chat intake on' }).click();
  await expect(page.getByRole('switch', { name: 'Chat intake on' })).toBeChecked();
  const intakeDefault = page.getByRole('group', { name: 'Intake: default for channels' });
  await expect(intakeDefault.getByRole('radio')).toHaveCount(2);
  await intakeDefault.getByRole('radio', { name: 'Suggest' }).click();
  await expect(intakeDefault.getByRole('radio', { name: 'Suggest' })).toBeChecked();

  // The channel's AI tab: its groups follow the default, so their settings are the default's until the
  // scope has its own level.
  await page.goto(`${origin}/settings/channels/${channel}?tab=ai`);
  const following = page.getByRole('form', { name: 'Intake settings · Groups' });
  await expect(following).toContainText('These follow the default. Pick Suggest or Auto for Groups above to set its own.');
  await expect(following.getByRole('button', { name: 'Save intake settings' })).toHaveCount(0);
  await page.getByRole('combobox', { name: 'Intake · Groups' }).selectOption('suggest');
  const form = page.getByRole('form', { name: 'Intake settings · Groups' });
  await form.getByLabel('Add to a conversation').fill('0.93');
  await form.getByLabel('Words that mark a secret').fill('mật khẩu\nOTP');
  await form.getByRole('button', { name: 'Save intake settings' }).click();
  await expect(form.getByRole('status')).toHaveText('Saved');
  await expect(page.getByRole('region', { name: 'Last 7 days' })).toContainText('No cards yet.');

  // Auto asks first: nothing is labelled yet, so every action stays a suggestion; an override needs a reason.
  await page.getByRole('combobox', { name: 'Intake · Groups' }).selectOption('auto');
  const dialog = page.getByRole('dialog', { name: 'Let AI decide on its own · Groups' });
  await expect(dialog).toContainText('Open a conversation: stays a suggestion until 30 decisions are labelled (0 of 30)');
  await expect(dialog).toContainText('No estimate yet');
  await dialog.getByRole('checkbox', { name: 'Let it open a conversation alone anyway' }).check();
  await dialog.getByRole('button', { name: 'Turn on Auto' }).click();
  await expect(dialog.getByRole('alert')).toContainText('Say why');
  await dialog.getByLabel('Why AI may do this before it is ready').fill('Checked 50 cards by hand');
  await dialog.getByRole('button', { name: 'Turn on Auto' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('combobox', { name: 'Intake · Groups' })).toHaveValue('auto');

  // One change kept the level, the override and the settings saved before.
  const levels = await (await page.request.get(`${origin}/api/v1/channels/${channel}/ai`)).json() as {
    scopes: { scope: string; slots: { slot: string; level: string | null; options: Record<string, unknown> | null }[] }[] };
  const intake = levels.scopes.find((entry) => entry.scope === 'group')!.slots.find((item) => item.slot === 'intake')!;
  expect(intake).toMatchObject({ level: 'auto', options: { attach_threshold: 0.93, redact_keywords: ['mật khẩu', 'OTP'], gate_override: ['create'],
    gate_override_reason: 'Checked 50 cards by hand' } });
  const readiness = await (await page.request.get(`${origin}/api/v1/channels/${channel}/ai/intake/readiness?scope=group`)).json() as { actions: { action: string; gate: string }[] };
  expect(readiness.actions.find((action) => action.action === 'create')?.gate).toBe('overridden');
});
