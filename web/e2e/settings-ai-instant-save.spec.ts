import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { createWorkspace, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import type { ChatServer } from './support/chat';
import { addLoopbackChannel, startChatServer } from './support/chat';

// Built-in AI and a channel's AI tab save a choice when it is made and say "Saved" with Undo; chat intake asks the
// person to agree before it turns on. Real server, real clicks; only the model behind the provider is a stand-in.
let chat: ChatServer;
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

interface AIState { slots: { slot: string; enabled: boolean }[]; source_consents: { source_class: string }[] }
const ai = async (page: Page): Promise<AIState> => (await (await page.request.get(`${chat.origin}/api/v1/ai`)).json()) as AIState;
const slotOn = async (page: Page, name: string) => (await ai(page)).slots.find((slot) => slot.slot === name)?.enabled;

// desk signs in as the admin of a new workspace with one chat channel and a provider, and returns the channel's id.
async function desk(page: Page): Promise<string> {
  const { origin } = chat;
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`AI instant ${suffix}`, `admin-${suffix}@example.com`, origin, chat.databaseURL));
  const channel = await addLoopbackChannel(page.request, origin, `instant-${suffix}`);
  const provider = await page.request.post(`${origin}/api/v1/ai/providers`, { headers: { Origin: origin },
    data: { kind: 'openai_compatible', label: 'Office model', base_url: `http://127.0.0.1:${(model.address() as AddressInfo).port}/v1`, models: ['llama-4-scout'] } });
  expect(provider.status(), await provider.text()).toBe(201);
  const id = ((await provider.json()) as { id: string }).id;
  for (const slot of ['draft', 'intake']) {
    expect((await page.request.put(`${origin}/api/v1/ai/slots/${slot}`, { headers: { Origin: origin }, data: { source: 'provider', provider_id: id, model: 'llama-4-scout', enabled: false } })).status()).toBe(200);
  }
  return channel;
}

test('a feature switch says Saved with Undo, and Undo puts the feature back', async ({ page }) => {
  await desk(page);
  await page.goto(`${chat.origin}/settings/ai`);
  const toggle = page.getByRole('switch', { name: 'Reply drafts on' });
  await toggle.click();
  const toast = page.getByRole('status').filter({ hasText: 'Saved' });
  await expect(toast).toBeVisible();
  await expect.poll(() => slotOn(page, 'draft')).toBe(true);
  await toast.getByRole('button', { name: 'Undo' }).click();
  await expect.poll(() => slotOn(page, 'draft')).toBe(false);
  await expect(toggle).not.toBeChecked();
  await expect(toast).toBeHidden();
});

test('turning chat intake on asks the person to agree; closing it leaves it off, agreeing turns it on and Undo turns it off', async ({ page }) => {
  await desk(page);
  await page.goto(`${chat.origin}/settings/ai`);
  await page.getByRole('switch', { name: 'Chat intake on' }).click();
  const consent = page.getByRole('dialog', { name: 'Turn on Chat intake' });
  await expect(consent).toBeVisible();
  await expect(consent).toContainText('What goes to the model');
  await expect(consent).toContainText('Office model, model llama-4-scout.');
  const agree = consent.getByRole('button', { name: 'Agree and turn on' });
  await expect(agree).toBeDisabled();
  // Closing without agreeing, with the key a person would press, leaves it off.
  await page.keyboard.press('Escape');
  await expect(consent).toBeHidden();
  expect(await slotOn(page, 'intake')).toBe(false);
  await expect(page.getByRole('switch', { name: 'Chat intake on' })).not.toBeChecked();

  await page.getByRole('switch', { name: 'Chat intake on' }).click();
  await consent.getByRole('checkbox', { name: /I agree that messages in linked chat channels are sent to Office model/ }).check();
  await expect(agree).toBeEnabled();
  await agree.click();
  await expect(consent).toBeHidden();
  await expect.poll(() => slotOn(page, 'intake')).toBe(true);
  const toast = page.getByRole('status').filter({ hasText: 'Saved' });
  await toast.getByRole('button', { name: 'Undo' }).click();
  await expect.poll(() => slotOn(page, 'intake')).toBe(false);
});

test('what intake may send besides the messages saves at once with Undo, and the Saved toast is above the open sheet', async ({ page }) => {
  await desk(page);
  await page.goto(`${chat.origin}/settings/ai`);
  await page.getByRole('button', { name: 'Chat intake settings' }).click();
  const sheet = page.getByRole('dialog', { name: 'Chat intake' });
  await sheet.getByRole('switch', { name: 'Images customers sent' }).click();
  await expect.poll(async () => (await ai(page)).source_consents.map((consent) => consent.source_class)).toEqual(['images']);
  const toast = page.getByRole('status').filter({ hasText: 'Saved' });
  await expect(toast).toBeVisible();
  // A real click: it reaches the toast only when nothing, the sheet's backdrop least of all, lies over it.
  await toast.getByRole('button', { name: 'Undo' }).click();
  await expect.poll(async () => (await ai(page)).source_consents).toEqual([]);
  await expect(sheet).toBeVisible();
});

test('a channel level picked on its AI tab says Saved with Undo, and Undo sends the level it had', async ({ page }) => {
  const channel = await desk(page);
  const { origin } = chat;
  expect((await page.request.put(`${origin}/api/v1/ai/slots/intake`, { headers: { Origin: origin }, data: { source: 'provider', provider_id: ((await ai(page)) as unknown as { providers: { id: string }[] }).providers[0].id, model: 'llama-4-scout', enabled: true } })).status()).toBe(200);
  const level = async () => {
    const levels = await (await page.request.get(`${origin}/api/v1/channels/${channel}/ai`)).json() as { scopes: { scope: string; slots: { slot: string; level: string | null }[] }[] };
    return levels.scopes.find((entry) => entry.scope === 'group')?.slots.find((item) => item.slot === 'intake')?.level ?? null;
  };
  await page.goto(`${origin}/settings/channels/${channel}?tab=ai`);
  const select = page.getByRole('combobox', { name: 'Intake · Groups' });
  await select.selectOption('suggest');
  await expect.poll(level).toBe('suggest');
  const toast = page.getByRole('status').filter({ hasText: 'Saved' });
  await expect(toast).toBeVisible();
  await select.selectOption('off');
  await expect.poll(level).toBe('off');
  await toast.getByRole('button', { name: 'Undo' }).click();
  await expect.poll(level).toBe('suggest');
  await expect(select).toHaveValue('suggest');
});

test('with a sheet open, Tab reaches Undo of the Saved toast and Enter on it takes the consent back', async ({ page }) => {
  await desk(page);
  await page.goto(`${chat.origin}/settings/ai`);
  await page.getByRole('button', { name: 'Chat intake settings' }).click();
  const sheet = page.getByRole('dialog', { name: 'Chat intake' });
  await sheet.getByRole('switch', { name: 'Images customers sent' }).click();
  await expect.poll(async () => (await ai(page)).source_consents.length).toBe(1);
  const undo = sheet.getByRole('button', { name: 'Undo' });
  await expect(undo).toBeVisible();
  let reached = false;
  for (let press = 0; press < 25 && !reached; press += 1) {
    await page.keyboard.press('Tab');
    reached = await undo.evaluate((element) => element === document.activeElement);
    // The sheet traps Tab: the focus never leaves it.
    expect(await page.evaluate(() => Boolean(document.activeElement?.closest('[role="dialog"]')))).toBe(true);
  }
  expect(reached, 'Undo is reached with Tab').toBe(true);
  await page.keyboard.press('Enter');
  await expect.poll(async () => (await ai(page)).source_consents).toEqual([]);
});

test('on a phone the Saved toast of a switch in the full-screen sheet sits above Done and does not cover it', async ({ page }) => {
  await desk(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${chat.origin}/settings/ai`);
  await page.getByRole('button', { name: 'Chat intake settings' }).click();
  const sheet = page.getByRole('dialog', { name: 'Chat intake' });
  await sheet.getByRole('switch', { name: 'Images customers sent' }).click();
  const toast = sheet.getByRole('status').filter({ hasText: 'Saved' });
  await expect(toast).toBeVisible();
  const done = sheet.getByRole('button', { name: 'Done' });
  const [toastBox, doneBox] = [(await toast.boundingBox())!, (await done.boundingBox())!];
  expect(toastBox.y + toastBox.height, 'the toast ends above Done').toBeLessThanOrEqual(doneBox.y);
  expect(toastBox.x, 'inside the screen').toBeGreaterThanOrEqual(0);
  expect(toastBox.x + toastBox.width, 'inside the screen').toBeLessThanOrEqual(390);
  for (const control of [toast.getByRole('button', { name: 'Undo' }), toast.getByRole('button', { name: 'Dismiss' })]) {
    expect((await control.boundingBox())!.height, 'a touch target').toBeGreaterThanOrEqual(44);
  }
  // A real click on Done reaches it: nothing lies over it.
  await done.click();
  await expect(sheet).toBeHidden();
});
