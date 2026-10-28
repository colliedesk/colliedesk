import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { mailpit, waitForMail } from './support/mailpit';

// Journey 3: docs/user/email-channel.md end to end on a server with a real SMTP relay (Mailpit).
const baseURL = servers.mail;

function rawEmail(lines: string[]): Buffer {
  return Buffer.from(lines.join('\r\n'));
}

test('email channel: forwarded mail opens a conversation, the reply is delivered by SMTP with threading headers, the customer reply threads back', { tag: '@onboarding' }, async ({ page }) => {
  const suffix = uniqueSuffix();
  const support = `support-${suffix}@acme.example`;
  const customer = `erin-${suffix}@customer.example`;
  await redeemLink(page, createWorkspace(`Mail Desk ${suffix}`, `admin-${suffix}@acme.example`, baseURL));

  // Step 2 of the guide: connect the support address and copy the inbound URL and secret.
  await page.getByRole('link', { name: 'Settings' }).click();
  await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Channels' }).click();
  await page.getByRole('link', { name: 'Connect your support email' }).click();
  await page.getByRole('textbox', { name: 'Support address' }).fill(support);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await page.getByRole('button', { name: 'Create inbound secret' }).click();
  const secret = await page.getByRole('textbox', { name: /Inbound secret/ }).inputValue();
  const inboundURL = await page.getByRole('textbox', { name: 'Inbound URL' }).inputValue();
  expect(inboundURL).toBe(`${baseURL}/api/v1/channels/email/inbound`);
  // How replies leave is on the channel's Replies tab.
  await page.getByRole('tab', { name: 'Replies' }).click();
  // The relay's host and port are a server detail, folded away for admins.
  await page.locator('details', { hasText: 'Outgoing email (SMTP)' }).getByText('Server details').click();
  await expect(page.getByText(`127.0.0.1:${mailpit.smtpPort} · STARTTLS off · no login`)).toBeVisible();
  await expect(page.getByText(`Replies are sent from ${support}.`)).toBeVisible();

  // Step 4: the forwarder posts the raw message, with an attachment, unchanged.
  const customerMessageID = `<first-${suffix}@customer.example>`;
  const inbound = await page.request.post(inboundURL, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret },
    data: rawEmail([
      `From: Erin Customer <${customer}>`, `To: ${support}`, `Subject: Order ${suffix} never arrived`,
      `Message-ID: ${customerMessageID}`, 'Date: Tue, 13 Oct 2026 23:30:00 +0000', 'MIME-Version: 1.0',
      'Content-Type: multipart/mixed; boundary="b1"', '',
      '--b1', 'Content-Type: text/plain; charset=utf-8', '', 'My order never arrived. Receipt attached.',
      '--b1', 'Content-Type: application/pdf; name="receipt.pdf"', 'Content-Disposition: attachment; filename="receipt.pdf"',
      'Content-Transfer-Encoding: base64', '', Buffer.from('%PDF-1.4 receipt bytes').toString('base64'), '--b1--', '',
    ]),
  });
  expect(inbound.status()).toBe(200);
  const created = await inbound.json() as { status: string; conversation_id: string };
  expect(created.status).toBe('created');

  await page.getByRole('link', { name: 'Inbox' }).click();
  await page.getByRole('button', { name: new RegExp(`Order ${suffix} never arrived`) }).click();
  await expect(page.getByText('My order never arrived. Receipt attached.')).toBeVisible();
  const receipt = page.getByRole('link', { name: 'receipt.pdf' });
  expect(await (await page.request.get(`${baseURL}${await receipt.getAttribute('href')}`)).text()).toBe('%PDF-1.4 receipt bytes');
  await expect(page.getByRole('status')).toHaveCount(0);

  // The agent replies; the outbox worker delivers it through SMTP and the timeline says Sent.
  await page.getByLabel('Message body').last().fill(`We are on it, Erin (${suffix}).`);
  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(page.getByText('Sent', { exact: true })).toBeVisible({ timeout: 30_000 });

  const delivered = await waitForMail(customer);
  expect(delivered.from).toBe(support);
  expect(delivered.subject).toBe(`Re: Order ${suffix} never arrived`);
  expect(delivered.text).toContain(`We are on it, Erin (${suffix}).`);
  expect(delivered.headers['In-Reply-To']).toEqual([customerMessageID]);
  expect(delivered.headers.References?.[0]).toContain(customerMessageID);
  const replyMessageID = delivered.headers['Message-Id']?.[0] ?? delivered.headers['Message-ID']?.[0];
  expect(replyMessageID).toMatch(/^<[^<>@\s]+@[^<>\s]+>$/);
  expect(delivered.headers['Reply-To']?.[0] ?? '').toContain(support.split('@')[1]);

  // The customer answers from their mail client: In-Reply-To names our reply.
  const answer = await page.request.post(inboundURL, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret },
    data: rawEmail([
      `From: Erin Customer <${customer}>`, `To: ${support}`, `Subject: Re: Order ${suffix} never arrived`,
      `Message-ID: <second-${suffix}@customer.example>`, `In-Reply-To: ${replyMessageID}`,
      `References: ${customerMessageID} ${replyMessageID}`, 'MIME-Version: 1.0', 'Content-Type: text/plain; charset=utf-8', '',
      'Thanks, it just arrived.', '',
    ]),
  });
  expect(answer.status()).toBe(200);
  expect(await answer.json()).toMatchObject({ status: 'threaded', conversation_id: created.conversation_id });

  await page.reload();
  await expect(page.getByText('Thanks, it just arrived.')).toBeVisible();
  await expect(page.getByRole('button', { name: new RegExp(`Order ${suffix} never arrived`) })).toHaveCount(1);
});
