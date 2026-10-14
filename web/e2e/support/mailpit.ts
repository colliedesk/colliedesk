import { expect } from '@playwright/test';

// Mailpit is the SMTP sink that stands in for the customer's mailbox (a real SMTP server).
export const mailpit = {
  smtpPort: process.env.MAILPIT_SMTP_PORT ?? '55425',
  api: process.env.MAILPIT_API_URL ?? 'http://127.0.0.1:55426',
};

interface Summary { ID: string; MessageID: string; Subject: string; From: { Address: string; Name: string }; To: Array<{ Address: string }> }
export interface DeliveredMail { id: string; subject: string; from: string; text: string; headers: Record<string, string[]> }

// waitForMail polls Mailpit until a message to the address arrives and returns it with its headers.
export async function waitForMail(to: string, timeout = 30_000): Promise<DeliveredMail> {
  let found: Summary | undefined;
  await expect.poll(async () => {
    const response = await fetch(`${mailpit.api}/api/v1/search?query=${encodeURIComponent(`to:"${to}"`)}`);
    const page = await response.json() as { messages: Summary[] };
    found = page.messages[0];
    return Boolean(found);
  }, { timeout, message: `mail to ${to} in Mailpit` }).toBe(true);
  const id = found!.ID;
  const message = await (await fetch(`${mailpit.api}/api/v1/message/${id}`)).json() as { Text: string; Subject: string; From: { Address: string } };
  const headers = await (await fetch(`${mailpit.api}/api/v1/message/${id}/headers`)).json() as Record<string, string[]>;
  return { id, subject: message.Subject, from: message.From.Address, text: message.Text, headers };
}
