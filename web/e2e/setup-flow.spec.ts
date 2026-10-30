import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { expectIdentityVisible, redeemLink } from './support/browser';

// The setup flow (docs/user/setup-guide.md, "Setup flow"): one step per screen, each done in place, Continue
// once the server reports the person's own part done, "I'll do this later" to move on, and a summary.
// The mail server relays replies by SMTP, so a saved address with its inbound secret waits for its first email.
const baseURL = servers.mail;

test('setup flow: a new admin connects email in place, skips the import, invites a teammate, reads the summary and opens the inbox', async ({ page }) => {
  const suffix = uniqueSuffix();
  const workspace = `Flow Desk ${suffix}`;
  const admin = `admin-${suffix}@acme.example`;
  await redeemLink(page, createWorkspace(workspace, admin, baseURL));
  // The moves between steps; the email setup has a Continue of its own for its own steps.
  const flow = page.getByRole('navigation', { name: 'Setup flow' });
  const continueButton = flow.getByRole('button', { name: 'Continue' });
  const later = flow.getByRole('link', { name: 'I\'ll do this later' });

  // /setup opens the first step still to do, full screen, with the workspace and address on screen.
  await page.goto(`${baseURL}/setup`);
  await expect(page).toHaveURL(`${baseURL}/setup/email`);
  await expect(page.getByRole('heading', { level: 1, name: 'Receive customer email' })).toBeVisible();
  await expect(page.getByRole('progressbar', { name: 'Setup progress' })).toHaveText('Step 1 of 4');
  await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0);
  await expectIdentityVisible(page, workspace, admin);
  await expect(continueButton).toBeDisabled();
  await expect(page.getByText('Save your support address above to continue.')).toBeVisible();

  // The address is saved here; the email setup goes on on this screen, not on the channel's Settings page.
  await page.getByRole('textbox', { name: 'Support address' }).fill(`support-${suffix}@acme.example`);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await expect(page.getByRole('group', { name: 'Forward its mail to Colliedesk' })).toContainText('Step 2 of 4');
  await expect(page).toHaveURL(`${baseURL}/setup/email`);
  // Without an inbound secret nothing can arrive yet: Continue still waits, and says so.
  await expect(continueButton).toBeDisabled();
  await expect(page.getByText('Your email needs attention. Finish the steps above to continue.')).toBeVisible();
  await page.getByRole('button', { name: 'Create inbound secret' }).click();
  await expect(page.getByRole('textbox', { name: /Inbound secret/ })).toBeVisible();
  // The server now reports the address waiting for its first email: the person's own part is done.
  await expect(continueButton).toBeEnabled();
  const setup = await (await page.request.get(`${baseURL}/api/v1/workspace/setup`)).json() as { steps: { key: string; state: string }[] };
  expect(setup.steps.find((step) => step.key === 'email')?.state).toBe('waiting');
  await continueButton.click();

  // The import can wait.
  await expect(page).toHaveURL(`${baseURL}/setup/import`);
  await expect(page.getByRole('heading', { level: 1, name: 'Import from Freshdesk' })).toBeVisible();
  await expect(page.getByRole('progressbar', { name: 'Setup progress' })).toHaveText('Step 2 of 4');
  await expect(page.getByLabel('Freshdesk address')).toBeVisible();
  await expect(continueButton).toBeDisabled();
  await later.click();

  // An invite is created here and its one-time link stays on screen.
  await expect(page).toHaveURL(`${baseURL}/setup/invite`);
  await expect(page.getByRole('heading', { level: 1, name: 'Invite your team' })).toBeVisible();
  await expect(continueButton).toBeDisabled();
  await page.getByLabel('Invite email').fill(`kim-${suffix}@acme.example`);
  await page.getByRole('button', { name: 'Create invite' }).click();
  const link = page.getByRole('textbox', { name: 'One-time invite link' });
  await expect(link).toHaveValue(new RegExp(`^${baseURL}/sign-in\\?token=`));
  await expect(continueButton).toBeEnabled();
  await expect(link).toBeVisible();
  await continueButton.click();

  await expect(page).toHaveURL(`${baseURL}/setup/first_reply`);
  await expect(page.getByRole('heading', { level: 1, name: 'Answer your first email' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Send yourself a test message' })).toBeVisible();
  // Back returns to the step before, as it was left.
  await flow.getByRole('link', { name: 'Back' }).click();
  await expect(page).toHaveURL(`${baseURL}/setup/invite`);
  await expect(continueButton).toBeEnabled();
  await continueButton.click();
  await later.click();

  // The summary: where each step stands, the way back to each unfinished one, and the inbox.
  await expect(page).toHaveURL(`${baseURL}/setup/done`);
  await expect(page.getByRole('heading', { level: 1, name: 'You\'re ready to answer customers' })).toBeVisible();
  const rows = page.getByRole('list', { name: 'Setup steps' }).getByRole('listitem');
  await expect(rows).toHaveText([/Receive customer email.*Waiting for the first email/, /Import from Freshdesk.*To do/, /Invite your team.*Invite sent/, /Answer your first email.*To do/]);
  await expect(rows.nth(1).getByRole('link', { name: 'Import from Freshdesk' })).toHaveAttribute('href', '/setup/import');
  await expectIdentityVisible(page, workspace, admin);
  await page.getByRole('link', { name: 'Open the inbox' }).click();
  await expect(page).toHaveURL(`${baseURL}/`);
  await expect(page.getByRole('navigation', { name: 'Main' })).toBeVisible();
});
