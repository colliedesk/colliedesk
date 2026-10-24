import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// docs/user/channels.md: add a channel, turn it off and on again, and what a reply does while its channel is off.
const baseURL = servers.main;
const site = 'https://shop.example.com';

test('channels: an admin adds a web form, turns it off (submissions refused) and on again, and a reply waits while email is off', async ({ page }) => {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Channel Desk ${suffix}`, `admin-${suffix}@example.com`, baseURL));

  // Add the web form from the empty Channels page.
  await page.getByRole('link', { name: 'Settings' }).click();
  await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Channels' }).click();
  await expect(page.getByRole('heading', { name: 'No channel yet' })).toBeVisible();
  await page.getByRole('link', { name: /Add a web form/ }).click();
  await page.getByRole('textbox', { name: 'Allowed origins' }).fill(site);
  await page.getByRole('button', { name: 'Save web form' }).click();
  await expect(page.getByRole('heading', { name: 'Web form', exact: true })).toBeVisible();
  const toggle = page.getByRole('switch', { name: 'Web form on' }).first();
  await expect(toggle).toBeChecked();

  const { submit_path: submitPath } = await (await page.request.get(`${baseURL}/api/v1/channels/web-form`)).json() as { submit_path: string };
  const submit = (who: string, message: string) => page.request.post(`${baseURL}${submitPath}`, {
    headers: { Origin: site, 'Content-Type': 'application/json' }, data: { email: `${who}-${suffix}@customer.example`, message, source: `${who}-${suffix}` },
  });
  expect((await submit('first', 'Where is my order?')).status()).toBe(202);
  // A switch answers at once; the channel has changed once its PATCH is answered.
  const changed = () => page.waitForResponse((response) => response.request().method() === 'PATCH' && /\/api\/v1\/channels\/[0-9a-f-]{36}$/.test(response.url()) && response.ok());

  // Off: asked first, then the form answers as if it did not exist, and the list reads Off.
  await toggle.click();
  const question = page.getByRole('alertdialog', { name: 'Turn Web form off?' });
  await expect(question).toContainText('Its settings and conversations stay');
  await Promise.all([changed(), question.getByRole('button', { name: 'Turn off' }).click()]);
  await expect(toggle).not.toBeChecked();
  expect((await submit('second', 'Anyone there?')).status()).toBe(404);
  await page.getByRole('link', { name: 'All channels' }).click();
  const list = page.getByRole('region', { name: 'Channels' });
  await expect(list).toContainText('Off');
  await expect(list.getByRole('switch', { name: 'Web form on' })).not.toBeChecked();

  // On again: the next submission opens a conversation.
  await Promise.all([changed(), list.getByRole('switch', { name: 'Web form on' }).click()]);
  await expect(list.getByRole('switch', { name: 'Web form on' })).toBeChecked();
  expect((await submit('third', 'Back again')).status()).toBe(202);

  // Connect email, the channel web form replies leave through, then turn it off.
  await page.getByRole('button', { name: 'Add channel' }).click();
  await page.getByRole('dialog', { name: 'Add channel' }).getByRole('button', { name: /Connect your support email/ }).click();
  await page.getByRole('textbox', { name: 'Support address' }).fill(`support-${suffix}@example.com`);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await expect(page.getByRole('heading', { name: 'Email', exact: true })).toBeVisible();
  await page.getByRole('switch', { name: 'Email on' }).click();
  await page.getByRole('alertdialog', { name: 'Turn Email off?' }).getByRole('button', { name: 'Turn off' }).click();
  await expect(page.getByRole('switch', { name: 'Email on' })).not.toBeChecked();

  // The composer says the reply waits; nothing written is lost.
  await page.getByRole('link', { name: 'Inbox' }).click();
  await page.getByRole('button', { name: new RegExp(`Web form: third-${suffix}`) }).click();
  await expect(page.getByText('Email is off: this reply is saved but not sent until the channel is on.')).toBeVisible();

  // On again: the notice goes away.
  await page.goto(`${baseURL}/settings/channels/type/email`);
  await page.getByRole('switch', { name: 'Email on' }).click();
  await expect(page.getByRole('switch', { name: 'Email on' })).toBeChecked();
  await page.getByRole('link', { name: 'Inbox' }).click();
  await page.getByRole('button', { name: new RegExp(`Web form: third-${suffix}`) }).click();
  await expect(page.getByText('Email is off: this reply is saved but not sent until the channel is on.')).toHaveCount(0);
});
