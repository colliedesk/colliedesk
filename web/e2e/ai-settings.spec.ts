import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

const baseURL = servers.main;

// fakeModelServer stands in for an OpenAI-compatible provider (a vLLM or Ollama on the operator's network):
// it answers every chat completion with JSON, except for the model "missing".
async function fakeModelServer() {
  const calls: string[] = [];
  const server = createServer((request, response) => {
    let body = '';
    request.on('data', (chunk: Buffer) => { body += chunk.toString(); });
    request.on('end', () => {
      const model = (JSON.parse(body || '{}') as { model?: string }).model ?? '';
      calls.push(model);
      response.setHeader('Content-Type', 'application/json');
      if (model === 'missing') {
        response.statusCode = 404;
        response.end(JSON.stringify({ error: { message: 'model not found' } }));
        return;
      }
      response.end(JSON.stringify({ id: 'fake', model, choices: [{ message: { content: '{"ok":true}' }, finish_reason: 'stop' }],
        usage: { prompt_tokens: 12, completion_tokens: 3 } }));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  return { url: `http://127.0.0.1:${(server.address() as AddressInfo).port}/v1`, calls, close: () => server.close() };
}

test('a self-hosted admin adds a provider on the network, turns reply drafts on, and sees who did it', async ({ page }) => {
  const model = await fakeModelServer();
  try {
    const suffix = `${Date.now()}-${process.pid}`;
    const { url } = createWorkspaceWithSlug(`E2E AI ${suffix}`, `admin-${suffix}@example.com`);
    await page.goto(url);
    await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
    await expect(page).toHaveURL(`${baseURL}/`);

    await page.goto(`${baseURL}/settings`);
    await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'AI', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'No AI provider on this server yet' })).toBeVisible();
    await expect(page.getByRole('switch', { name: 'Reply drafts on' })).toBeDisabled();

    await page.getByRole('button', { name: 'Add provider' }).first().click();
    const dialog = page.getByRole('dialog', { name: 'Add a provider' });
    await dialog.getByText('OpenAI-compatible', { exact: true }).click();
    await dialog.getByLabel('Name', { exact: true }).fill('Office model');
    await dialog.getByLabel('Base URL', { exact: true }).fill(model.url);
    await dialog.getByLabel('Models', { exact: true }).fill('missing');
    await dialog.getByRole('button', { name: 'Check and save' }).click();
    await expect(dialog.getByText(/has no model with that name/)).toBeVisible();

    await dialog.getByLabel('Models', { exact: true }).fill('llama-4-scout, qwen-3');
    await dialog.getByRole('button', { name: 'Check and save' }).click();
    await expect(dialog).toBeHidden();
    await expect(page.getByText('Office model')).toBeVisible();
    await expect(page.getByText('Working')).toBeVisible();
    expect(model.calls).toEqual(['missing', 'llama-4-scout']);

    await page.getByRole('tab', { name: 'Features' }).click();
    await page.getByRole('combobox', { name: 'Reply drafts provider' }).selectOption({ label: 'Office model' });
    await expect(page.getByRole('combobox', { name: 'Reply drafts model' })).toHaveValue('llama-4-scout');
    await page.getByRole('switch', { name: 'Reply drafts on' }).click();
    await expect(page.getByRole('switch', { name: 'Reply drafts on' })).toBeChecked();

    await page.getByRole('tab', { name: 'History' }).click();
    await expect(page.getByText(/turned on Reply drafts on Office model/)).toBeVisible();
    await expect(page.getByText(/added Office model/)).toBeVisible();

    // Deleting the provider turns the feature off and says so before it happens.
    await page.getByRole('tab', { name: /Providers/ }).click();
    await page.getByRole('button', { name: 'Delete Office model' }).click();
    const confirm = page.getByRole('alertdialog', { name: 'Delete Office model?' });
    await expect(confirm.getByRole('listitem')).toHaveText('Reply drafts');
    await confirm.getByRole('button', { name: 'Delete provider' }).click();
    await page.getByRole('tab', { name: 'Features' }).click();
    await expect(page.getByRole('switch', { name: 'Reply drafts on' })).not.toBeChecked();
  } finally {
    model.close();
  }
});
