import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { waitForMail } from './support/mailpit';

// docs/user/email-channel.md: Settings → Email walks a new workspace through four steps on one page. Each
// step's state is the server's: the first forwarded mail flips step 2 without a reload, and the test
// message's reply, delivered by SMTP (Mailpit), finishes step 4.
const baseURL = servers.mail;

test('email setup: a new workspace saves its address, the first forwarded mail flips step 2 without a reload, and the delivered test reply finishes step 4', async ({ page }) => {
  // The first email shows on the app's inbox poll, every 20 s.
  test.setTimeout(120_000);
  const suffix = uniqueSuffix();
  const admin = `admin-${suffix}@acme.example`;
  const support = `support-${suffix}@acme.example`;
  await redeemLink(page, createWorkspace(`Setup Desk ${suffix}`, admin, baseURL));

  // Settings → Email: without an address, the page is the setup at step 1.
  await page.getByRole('link', { name: 'Settings' }).click();
  await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Email', exact: true }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/channels/type/email`);
  await expect(page.getByRole('heading', { name: 'Connect your support email' })).toBeVisible();
  const address = page.getByRole('group', { name: 'Your support address' });
  await expect(address).toContainText('Step 1 of 4');
  await expect(page.getByRole('group', { name: 'Forward its mail to Colliedesk' })).toHaveAttribute('aria-disabled', 'true');
  await address.getByRole('textbox', { name: 'Support address' }).fill(support);
  await address.getByRole('button', { name: 'Save support address' }).click();
  await page.waitForURL(/\/settings\/channels\/[0-9a-f-]{36}\/setup$/);

  // Step 2 on a server without forwarding addresses: Other / own server, with the inbound secret.
  const forward = page.getByRole('group', { name: 'Forward its mail to Colliedesk' });
  await expect(forward).toContainText('Step 2 of 4');
  await expect(address).toContainText(support);
  await expect(forward.getByRole('tab', { name: 'Other / own server' })).toHaveAttribute('aria-selected', 'true');
  await forward.getByRole('button', { name: 'Create inbound secret' }).click();
  const secret = await forward.getByRole('textbox', { name: /Inbound secret/ }).inputValue();
  const inboundURL = await forward.getByRole('textbox', { name: 'Inbound URL' }).inputValue();
  await expect(forward.getByText('Waiting for first email')).toBeVisible();
  await expect(forward.getByRole('button', { name: 'Continue' })).toBeDisabled();
  await expect(forward.getByRole('button', { name: 'Skip for now' })).toBeEnabled();
  await expect(page.getByRole('complementary', { name: 'Customers see' })).toContainText(`To: ${support}`);

  // The forwarder posts the first customer email; the step flips on the inbox poll, with no reload.
  await page.evaluate(() => { (window as unknown as { stillHere: boolean }).stillHere = true; });
  const inbound = await page.request.post(inboundURL, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': secret },
    data: Buffer.from([`From: Erin Customer <erin-${suffix}@customer.example>`, `To: ${support}`, `Subject: First one ${suffix}`,
      `Message-ID: <first-${suffix}@customer.example>`, 'MIME-Version: 1.0', 'Content-Type: text/plain; charset=utf-8', '', 'Hello there.', ''].join('\r\n')),
  });
  expect(inbound.status()).toBe(200);
  await expect(forward.getByText(/^First email received/)).toBeVisible({ timeout: 45_000 });
  expect(await page.evaluate(() => (window as unknown as { stillHere?: boolean }).stillHere)).toBe(true);
  await expect(forward.getByRole('button', { name: 'Continue' })).toBeEnabled();

  // Continue moves to the next step to do on the same page: replies already leave by the server's relay
  // (step 3 done), so step 4 opens. Measured from the click to the open step on screen.
  const stepChange = await page.evaluate(async () => {
    const button = Array.from(document.querySelectorAll<HTMLButtonElement>('.setup-open button')).find((item) => item.textContent === 'Continue')!;
    const started = performance.now();
    button.click();
    await new Promise<void>((resolve) => {
      const look = () => (document.querySelector('.setup-open')?.textContent?.includes('Step 4 of 4') ? resolve() : requestAnimationFrame(look));
      look();
    });
    return performance.now() - started;
  });
  test.info().annotations.push({ type: 'step change ms', description: stepChange.toFixed(1) });
  console.log(`email setup step change: ${stepChange.toFixed(1)} ms`);
  expect(stepChange).toBeLessThan(200);
  await expect(page.getByRole('group', { name: new RegExp(`Send replies from ${support}`) })).toContainText(`Replies are sent from ${support}.`);

  // Step 4: the test message opens its conversation; the reply goes out by SMTP.
  const setupURL = page.url();
  const test4 = page.getByRole('group', { name: 'Try it with a test message' });
  await expect(test4).toContainText('Step 4 of 4');
  await test4.getByRole('button', { name: 'Send yourself a test message' }).click();
  await expect(page).toHaveURL(/\/conversations\/[0-9a-f-]{36}$/);
  const reply = `Answering my test (${suffix}).`;
  await page.getByLabel('Message body').last().fill(reply);
  await page.getByRole('button', { name: 'Send reply' }).click();
  await expect(page.getByText('Sent', { exact: true })).toBeVisible({ timeout: 30_000 });
  const delivered = await waitForMail(admin, 30_000, suffix);
  expect(delivered.text).toContain(reply);

  // Back in the setup, step 4 is done because its reply was delivered, and so is the setup.
  await page.goto(setupURL);
  await expect(page.getByRole('group', { name: 'Try it with a test message' })).toContainText('Reply delivered');
  await expect(page.getByRole('status', { name: 'Email is set up' })).toBeVisible();

  // Settings → Email is now the table of addresses.
  await page.goto(`${baseURL}/settings/channels/type/email`);
  const row = page.getByRole('table', { name: 'Support addresses' }).getByRole('row', { name: new RegExp(support) });
  await expect(row).toContainText('Working');
  await expect(row.getByRole('link', { name: `Manage ${support}` })).toBeVisible();
});
