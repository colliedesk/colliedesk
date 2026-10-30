import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { waitForMail } from './support/mailpit';

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
  const toggle = page.getByRole('switch', { name: 'Web form' }).first();
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
  await expect(list.getByRole('switch', { name: 'Web form' })).not.toBeChecked();

  // On again: the next submission opens a conversation.
  await Promise.all([changed(), list.getByRole('switch', { name: 'Web form' }).click()]);
  await expect(list.getByRole('switch', { name: 'Web form' })).toBeChecked();
  expect((await submit('third', 'Back again')).status()).toBe(202);

  // Connect email, the channel web form replies leave through, then turn it off.
  await page.getByRole('button', { name: 'Add channel' }).click();
  await page.getByRole('dialog', { name: 'Add channel' }).getByRole('button', { name: /Connect your support email/ }).click();
  await page.getByRole('textbox', { name: 'Support address' }).fill(`support-${suffix}@example.com`);
  await page.getByRole('button', { name: 'Save support address' }).click();
  // Saving opens the setup of the new address; its switch is on the address's own page.
  await expect(page.getByRole('heading', { name: 'Email', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Manage this address' }).click();
  await page.getByRole('switch', { name: 'Email' }).click();
  await page.getByRole('alertdialog', { name: 'Turn Email off?' }).getByRole('button', { name: 'Turn off' }).click();
  await expect(page.getByRole('switch', { name: 'Email' })).not.toBeChecked();

  // The composer says the reply waits; nothing written is lost.
  await page.getByRole('link', { name: 'Inbox' }).click();
  await page.getByRole('button', { name: new RegExp(`Web form: third-${suffix}`) }).click();
  await expect(page.getByText('Email is off: this reply is saved but not sent until the channel is on.')).toBeVisible();

  // On again: the notice goes away.
  // Settings → Email lists the address; Manage on its row opens it.
  await page.goto(`${baseURL}/settings/channels/type/email`);
  await page.getByRole('table', { name: 'Support addresses' }).getByRole('link', { name: `Manage support-${suffix}@example.com` }).click();
  await page.getByRole('switch', { name: 'Email' }).click();
  await expect(page.getByRole('switch', { name: 'Email' })).toBeChecked();
  await page.getByRole('link', { name: 'Inbox' }).click();
  await page.getByRole('button', { name: new RegExp(`Web form: third-${suffix}`) }).click();
  await expect(page.getByText('Email is off: this reply is saved but not sent until the channel is on.')).toHaveCount(0);
});

// docs/user/channels.md "Add a channel" and email-channel.md "Several mailboxes": a second mailbox,
// refused first for an address the first one has, made the default; a new conversation sent from the
// first; mail to the second answered from it; two forms on one site.
test('channels: a second mailbox is its own channel, a new conversation leaves from the one chosen, and its replies leave from the mailbox the mail reached', async ({ page }) => {
  const suffix = uniqueSuffix();
  const support = `support-${suffix}@acme.example`;
  const billing = `billing-${suffix}@acme.example`;
  const mailURL = servers.mail;
  await redeemLink(page, createWorkspace(`Two Mailboxes ${suffix}`, `admin-${suffix}@acme.example`, mailURL));
  const secretOf = async () => {
    await page.getByRole('button', { name: 'Create inbound secret' }).click();
    return page.getByRole('textbox', { name: /Inbound secret/ }).inputValue();
  };

  await page.goto(`${mailURL}/settings/channels/new/email`);
  await page.getByRole('textbox', { name: 'Support address' }).fill(support);
  await page.getByRole('button', { name: 'Save support address' }).click();
  // The channel exists once its setup opens.
  const channelSetup = /\/settings\/channels\/[0-9a-f-]{36}\/setup$/;
  await page.waitForURL(channelSetup);
  const emailPage = page.url().replace(/\/setup$/, '');

  // The same mailbox with a +tag is refused at the address, with the channel that has it.
  await page.goto(`${mailURL}/settings/channels`);
  await page.getByRole('button', { name: 'Add channel' }).click();
  await page.getByRole('dialog', { name: 'Add channel' }).getByRole('button', { name: /Connect your support email/ }).click();
  await page.getByRole('textbox', { name: 'Name' }).fill('Billing');
  const address = page.getByRole('textbox', { name: 'Support address' });
  await address.fill(`support-${suffix}+billing@acme.example`);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await expect(address).toHaveAttribute('aria-invalid', 'true');
  await expect(page.getByRole('link', { name: 'Open Email' })).toHaveAttribute('href', new URL(emailPage).pathname);
  await address.fill(billing);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await page.waitForURL(channelSetup);
  await page.getByRole('link', { name: 'Manage this address' }).click();
  await expect(page.getByRole('heading', { name: 'Billing', exact: true })).toBeVisible();
  const billingSecret = await secretOf();
  const inboundURL = await page.getByRole('textbox', { name: 'Inbound URL' }).inputValue();

  // Billing becomes the default; the list marks it.
  await page.getByRole('button', { name: 'Make default' }).click();
  await expect(page.getByText('Billing is now the default', { exact: false })).toBeVisible();
  await page.getByRole('link', { name: 'All channels' }).click();
  const list = page.getByRole('region', { name: 'Channels' });
  await expect(list.getByText('Default', { exact: true })).toHaveCount(1);
  await expect(list.locator('.ch-row', { hasText: billing })).toContainText('Default');

  // A new conversation asks which mailbox sends it, the default chosen; it leaves from the one picked.
  const prospect = `pat-${suffix}@prospect.example`;
  await page.getByRole('link', { name: 'Inbox' }).click();
  await page.getByRole('button', { name: 'New conversation' }).click();
  const start = page.getByRole('dialog', { name: 'Start a conversation' });
  const from = start.getByRole('combobox', { name: 'Send from' });
  await expect(from.locator('option:checked')).toHaveText('Billing');
  await from.selectOption({ label: 'Email' });
  await start.getByRole('button', { name: 'Add a new customer' }).click();
  await start.getByRole('textbox', { name: 'Customer name' }).fill('Pat');
  await start.getByRole('textbox', { name: 'Customer email' }).fill(prospect);
  await start.getByRole('textbox', { name: 'Subject' }).fill(`Renewal ${suffix}`);
  await start.getByRole('textbox', { name: 'Message' }).fill('Your plan renews next week.');
  await start.getByRole('button', { name: 'Send' }).click();
  expect((await waitForMail(prospect)).from).toBe(support);
  await expect(page.getByText(`Replies send from Email.`)).toBeVisible();

  // Mail to Billing opens its conversation there, and the reply leaves from Billing.
  const customer = `erin-${suffix}@customer.example`;
  const inbound = await page.request.post(inboundURL, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': billingSecret },
    data: Buffer.from([`From: Erin <${customer}>`, `To: ${billing}`, `Subject: Invoice ${suffix}`, `Message-ID: <invoice-${suffix}@customer.example>`,
      'MIME-Version: 1.0', 'Content-Type: text/plain; charset=utf-8', '', 'Where is my invoice?', ''].join('\r\n')),
  });
  expect(inbound.status()).toBe(200);
  await page.getByRole('link', { name: 'Inbox' }).click();
  await page.getByRole('button', { name: new RegExp(`Invoice ${suffix}`) }).click();
  await expect(page.getByText('Replies send from Billing.')).toBeVisible();
  await page.getByLabel('Message body').last().fill(`Attached, Erin (${suffix}).`);
  await page.getByRole('button', { name: 'Send reply' }).click();
  expect((await waitForMail(customer)).from).toBe(billing);

  // The inbox lists one mailbox's conversations.
  await page.locator('summary', { hasText: 'Filter' }).click();
  await page.getByRole('group', { name: 'Email channels' }).getByRole('checkbox', { name: 'Billing' }).check();
  await expect(page.getByRole('button', { name: new RegExp(`Invoice ${suffix}`) })).toBeVisible();
  await expect(page.getByRole('button', { name: new RegExp(`Renewal ${suffix}`) })).toHaveCount(0);

  // Two forms on the same site, each at its own address.
  const forms = await Promise.all(['Contact form', 'Careers form'].map(async (name) => {
    const response = await page.request.post(`${mailURL}/api/v1/channels`, { headers: { Origin: mailURL }, data: { type: 'web_form', name, config: { allowed_origins: [site] } } });
    expect(response.status()).toBe(201);
    return (await response.json() as { config: { submit_path: string } }).config.submit_path;
  }));
  expect(forms[0]).not.toBe(forms[1]);
  for (const path of forms) {
    const posted = await page.request.post(`${mailURL}${path}`, { headers: { Origin: site, 'Content-Type': 'application/json' }, data: { email: `visitor-${suffix}@example.com`, message: 'Hi' } });
    expect(posted.status()).toBe(202);
  }
});
