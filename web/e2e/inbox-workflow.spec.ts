import { execFileSync } from 'node:child_process';
import { expect, test } from '@playwright/test';
import { databaseURL, goEnv, servers } from './support/cli';

const baseURL = servers.main;

test('redeems a CLI pairing link and completes the inbox workflow', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const pairingURL = execFileSync('go', [
    'run', './cmd/collie', 'create-workspace',
    '--name', `E2E Desk ${suffix}`, '--admin-email', `admin-${suffix}@example.com`,
  ], {
    cwd: '../server', encoding: 'utf8',
    env: {
      ...process.env, APP_ORIGIN: baseURL, DATABASE_URL: databaseURL,
      ...goEnv,
    },
  }).trim();

  await page.goto(pairingURL);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${baseURL}/`);

  const headers = { Origin: baseURL };
  const contactResponse = await page.request.post(`${baseURL}/api/v1/contacts`, {
    headers, data: { name: 'E2E Customer', email: `customer-${suffix}@example.com` },
  });
  expect(contactResponse.status()).toBe(201);
  const contact = await contactResponse.json() as { id: string };
  const conversationResponse = await page.request.post(`${baseURL}/api/v1/conversations`, {
    headers, data: { contact_id: contact.id, subject: 'E2E inbox workflow' },
  });
  expect(conversationResponse.status()).toBe(201);
  const conversation = await conversationResponse.json() as { id: string };
  const currentConversation = async () => {
    const response = await page.request.get(`${baseURL}/api/v1/conversations/${conversation.id}`);
    expect(response.status()).toBe(200);
    return response.json() as Promise<{ status: string; assignee_id: string | null; tags: Array<{ id: string }> }>;
  };
  await page.reload();
  await page.getByRole('button', { name: /E2E inbox workflow/i }).click();

  const composer = page.getByLabel('Message body').last();
  await composer.fill('E2E public reply');
  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(page.getByText('E2E public reply')).toBeVisible();
  await page.getByRole('button', { name: 'Private note', exact: true }).click();
  await composer.fill('E2E private note');
  await page.getByRole('button', { name: 'Add private note' }).click();
  await expect(page.getByText('E2E private note')).toBeVisible();

  const details = page.getByRole('complementary', { name: 'Conversation details' });
  // Status is a menu: a status is set only once it is chosen (arrow keys on a select would set each one).
  const setStatus = async (name: string) => {
    await details.locator('.status-pick summary').click();
    await details.getByRole('group', { name: 'Status' }).getByRole('button', { name }).click();
  };
  await setStatus('Resolved');
  await expect.poll(async () => (await currentConversation()).status).toBe('resolved');
  await setStatus('Open');
  await expect.poll(async () => (await currentConversation()).status).toBe('open');

  // Send and resolve: the reply goes out first, then the conversation is resolved, without the details pane.
  await page.getByRole('button', { name: 'Reply', exact: true }).click();
  await composer.fill('E2E reply that resolves');
  await page.getByRole('button', { name: 'Send and resolve' }).click();
  await expect(page.getByText('E2E reply that resolves')).toBeVisible();
  await expect.poll(async () => (await currentConversation()).status).toBe('resolved');
  // The thread header offers the way back, and Resolve again, one press each.
  await page.getByRole('button', { name: 'Reopen' }).click();
  await expect.poll(async () => (await currentConversation()).status).toBe('open');
  await composer.fill('E2E reply sent with the keyboard');
  await composer.press('ControlOrMeta+Shift+Enter');
  await expect(page.getByText('E2E reply sent with the keyboard')).toBeVisible();
  await expect.poll(async () => (await currentConversation()).status).toBe('resolved');
  await page.getByRole('button', { name: 'Reopen' }).click();
  await expect.poll(async () => (await currentConversation()).status).toBe('open');
  await page.getByRole('button', { name: 'Resolve', exact: true }).click();
  await expect.poll(async () => (await currentConversation()).status).toBe('resolved');
  await page.getByRole('button', { name: 'Reopen' }).click();
  await expect.poll(async () => (await currentConversation()).status).toBe('open');

  const meResponse = await page.request.get(`${baseURL}/api/v1/auth/me`);
  const me = await meResponse.json() as { agent: { id: string } };
  const assignee = details.getByLabel('Assignee');
  await assignee.selectOption(me.agent.id);
  await expect.poll(async () => (await currentConversation()).assignee_id).toBe(me.agent.id);
  await assignee.selectOption('');
  await expect.poll(async () => (await currentConversation()).assignee_id).toBeNull();

  await details.getByLabel('New tag name').fill('E2E tag');
  await details.getByRole('button', { name: 'Create tag' }).click();
  const tag = details.getByRole('checkbox', { name: 'E2E tag' });
  await expect(tag).toBeChecked();
  await expect.poll(async () => (await currentConversation()).tags.length).toBe(1);
  await tag.click();
  await expect(tag).not.toBeChecked();
  await expect.poll(async () => (await currentConversation()).tags).toEqual([]);
});
