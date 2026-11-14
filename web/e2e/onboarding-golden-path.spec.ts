import { expect, test } from '@playwright/test';
import { openQuickStart } from './support/browser';
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
    await expect(page.getByRole('region', { name: 'Plan notices' })).toContainText(/Trial ends [A-Z][a-z]{2} \d{1,2}/);
    await (await openQuickStart(page)).getByRole('link', { name: /Import from Freshdesk/ }).click();
    await expect(page).toHaveURL(`${baseURL}/setup/import`);

    // Import in the app, in the background, until the reconcile report is ready and OK.
    await page.getByLabel('Freshdesk address').fill('acme.freshdesk.com');
    await page.getByLabel('API key of a Freshdesk admin', { exact: true }).fill(apiKey);
    await page.getByRole('button', { name: 'Read account' }).click();
    await expect(page.getByRole('heading', { name: 'Found in acme.freshdesk.com' })).toBeVisible({ timeout: 60_000 });
    await page.getByRole('button', { name: 'Start import' }).click();
    await expect(page.getByRole('heading', { name: 'Import complete' })).toBeVisible({ timeout: 180_000 });
    await expect(page.getByRole('row', { name: /^Tickets/ }).getByRole('cell')).toHaveText([String(fixture.tickets), String(fixture.tickets), '—']);
    const imports = await (await page.request.get(`${baseURL}/api/v1/imports`)).json() as { items: Array<{ kind: string; status: string | null }> };
    expect(imports.items.filter((job) => job.kind === 'import').map((job) => job.status)).toEqual(['OK']);

    // AI drafts and reply targets come after the import in the setup flow (#514). The sign-up goal (leave a
    // helpdesk) put the import first; the email step waits for later. Drafts turn on with the server's built-in AI
    // (the same call as Settings → Built-in AI, so the consent is recorded), triage stays off, and the suggested
    // preset is applied: the imported history is older than 30 days, so Same day is suggested.
    const flow = page.getByRole('navigation', { name: 'Setup flow' });
    await flow.getByRole('button', { name: 'Next: Receive customer email' }).click();
    await flow.getByRole('link', { name: "I'll do this later" }).click();
    await expect(page).toHaveURL(`${baseURL}/setup/ai_drafts`);
    await expect(page.getByRole('heading', { level: 1, name: 'Let AI draft your replies' })).toBeVisible();
    await flow.getByRole('button', { name: 'Turn on AI drafts' }).click();
    await expect(page.getByText('The first draft comes with the next customer message.')).toBeVisible();
    await flow.getByRole('button', { name: 'Next: Set reply targets' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Choose when a reply is due' })).toBeVisible();
    await expect(page.getByText(/Your import has no answered conversation from the last 30 days to learn from, so we suggest Same day/)).toBeVisible();
    await expect(page.getByRole('radio', { name: /Same day/ })).toBeChecked();
    await flow.getByRole('button', { name: 'Use Same day' }).click();
    await expect(page).toHaveURL(`${baseURL}/setup/invite`);
    const ai = await (await page.request.get(`${baseURL}/api/v1/ai`)).json() as { slots: Array<{ slot: string; source: string; enabled: boolean; default_level: string; consented_at: string | null }> };
    expect(ai.slots.find((slot) => slot.slot === 'draft')).toMatchObject({ source: 'server', enabled: true, default_level: 'suggest', consented_at: expect.any(String) as unknown });
    expect(ai.slots.find((slot) => slot.slot === 'triage')).toMatchObject({ enabled: false });
    const targets = await (await page.request.get(`${baseURL}/api/v1/response-targets`)).json() as Record<string, unknown>;
    expect(targets).toMatchObject({ enabled: true, warn_before_minutes: 30, targets: { urgent: { first_response_minutes: 60, resolution_minutes: 240 }, normal: { first_response_minutes: 480, resolution_minutes: 1620 } } });

    // The results page shows the baseline measured from the import (#100). Every ticket of the fixture got
    // its first reply 20 minutes after the customer's first message (not after the follow-up at 10), and
    // the resolved and closed ones were resolved at 40 minutes (Freshdesk stats, read with the list).
    // The import ran in the setup flow, which has no rail: leaving it brings the rail back.
    await page.getByRole('link', { name: 'Exit setup' }).click();
    await page.getByRole('link', { name: 'Reports' }).click();
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
    // Back to app returns to Reports, where the team was before Settings; the inbox is one place over.
    await page.getByRole('link', { name: 'Back to app' }).click();
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
