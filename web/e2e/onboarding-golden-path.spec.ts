import { expect, test } from '@playwright/test';
import { servers, uniqueSuffix } from './support/cli';
import { FakeFreshdesk, fixture, requesterEmail, ticketSubject } from './support/fake-freshdesk';
import { IdentityProvider } from './support/identity-provider';
import { waitForMail } from './support/mailpit';
import { signUp } from './support/signup';

// The golden path (#105): the first hour of a team leaving Freshdesk, on the hosted cloud, in one test.
// It signs up, imports the account in the app, sees the reconcile report OK and answers its first
// conversation by email. The stand-ins are the identity provider, the Freshdesk API v2 and Mailpit as the
// SMTP relay. Later steps extend this test, never a copy of it:
//   #99: the AI backtest is ready, after the baseline (with its own time-to-backtest usage field).
const baseURL = servers.cloud;
const apiKey = 'fd-e2e-golden-path-key-never-shown';
const provider = new IdentityProvider();

test.beforeAll(async () => { await provider.start(); });
test.afterAll(async () => { await provider.stop(); });

test('golden path: a new team signs up on the cloud, imports Freshdesk in the app, reconcile OK, answers its first conversation by email', { tag: '@onboarding' }, async ({ page }) => {
  test.setTimeout(240_000);
  const suffix = uniqueSuffix();
  const adminEmail = `lead-${suffix}@acme.example`;
  const fake = new FakeFreshdesk({ apiKey, agentEmail: adminEmail, fixed: true });
  await fake.start();
  try {
    // Sign up: the trial opens on the empty inbox, which leads to the import.
    expect((await signUp(page, provider, adminEmail, `Golden Desk ${suffix}`)).status()).toBe(201);
    await page.goto(baseURL);
    await expect(page.getByRole('region', { name: 'Plan notices' })).toContainText('Trial: 14 days left');
    await page.getByRole('region', { name: 'Finish setting up' }).getByRole('link', { name: /Import from Freshdesk/ }).click();
    await expect(page).toHaveURL(`${baseURL}/setup/import`);

    // Import in the app, in the background, until the reconcile report is ready and OK.
    await page.getByLabel('Freshdesk address').fill('acme.freshdesk.com');
    await page.getByLabel('API key of a Freshdesk admin').fill(apiKey);
    await page.getByRole('button', { name: 'Read account' }).click();
    await expect(page.getByRole('heading', { name: 'Found in acme.freshdesk.com' })).toBeVisible({ timeout: 60_000 });
    await page.getByRole('button', { name: 'Start import' }).click();
    await expect(page.getByRole('heading', { name: 'Import complete' })).toBeVisible({ timeout: 180_000 });
    await expect(page.getByRole('row', { name: /^Tickets/ }).getByRole('cell')).toHaveText([String(fixture.tickets), String(fixture.tickets), '—']);
    const imports = await (await page.request.get(`${baseURL}/api/v1/imports`)).json() as { items: Array<{ kind: string; status: string | null }> };
    expect(imports.items.filter((job) => job.kind === 'import').map((job) => job.status)).toEqual(['OK']);

    // The results page shows the baseline measured from the import (#100). Every ticket of the fixture got
    // its first reply 20 minutes after the customer's first message (not after the follow-up at 10), and
    // the resolved and closed ones were resolved at 40 minutes (Freshdesk stats, read with the list).
    // The import ran in the setup flow, which has no rail: leaving it brings the rail back.
    await page.getByRole('link', { name: 'Exit setup' }).click();
    await page.getByRole('link', { name: 'Results' }).click();
    await expect(page).toHaveURL(`${baseURL}/results`);
    const firstResponse = page.getByRole('region', { name: 'First response time' });
    await expect(firstResponse.getByRole('link', { name: '20 min' }).first()).toBeVisible({ timeout: 60_000 });
    await expect(firstResponse.getByRole('link', { name: `${fixture.tickets} answered` })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Time to resolve' }).getByRole('link', { name: '40 min' }).first()).toBeVisible();
    await expect(page.getByText('Freshdesk, before you moved')).toBeVisible();

    // Connect the support address: replies now go out by email.
    const support = `support-${suffix}@acme.example`;
    await page.getByRole('link', { name: 'Settings' }).click();
    await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Channels' }).click();
    await page.getByRole('link', { name: 'Connect your support email' }).click();
    await page.getByRole('textbox', { name: 'Support address' }).fill(support);
    await page.getByRole('button', { name: 'Save support address' }).click();
    // The setup opens at forwarding; how replies leave is its step 3, done already by the server's relay.
    await expect(page.getByRole('group', { name: new RegExp(`Send replies from ${support}`) })).toContainText(`Replies are sent from ${support}.`);

    // Answer a first conversation: a customer who wrote in Freshdesk (ticket 296, open, an active contact)
    // gets the reply by SMTP from the support address.
    const subject = ticketSubject(296);
    const customer = requesterEmail(296)!;
    await page.getByRole('link', { name: 'Inbox' }).click();
    await page.getByRole('button', { name: new RegExp(subject) }).click();
    const reply = `Thanks for your patience, this is Golden Desk (${suffix}).`;
    await page.getByLabel('Message body').last().fill(reply);
    await page.getByRole('button', { name: 'Send reply' }).click();
    await expect(page.getByText('Sent', { exact: true })).toBeVisible({ timeout: 30_000 });

    const delivered = await waitForMail(customer, 30_000, suffix);
    expect(delivered.from).toBe(support);
    expect(delivered.subject).toBe(`Re: ${subject}`);
    expect(delivered.text).toContain(reply);
  } finally {
    await fake.stop();
  }
});
