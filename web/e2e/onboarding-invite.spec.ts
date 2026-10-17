import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { appNavigationWith, expectReadablePage, redeemLink } from './support/browser';

// Journey 2: docs/user/invites.md and docs/user/sessions.md with two real browsers.
const baseURL = servers.main;
const headers = { Origin: baseURL };

test('invite teammate: admin invites, teammate joins under the agent ceiling, admin revokes, revoked session refused', { tag: '@onboarding' }, async ({ browser }) => {
  const suffix = uniqueSuffix();
  const adminEmail = `admin-${suffix}@example.com`;
  const teammateEmail = `Sam.${suffix}@Example.com`;
  const adminContext = await browser.newContext();
  const admin = await adminContext.newPage();
  await redeemLink(admin, createWorkspace(`Invite Desk ${suffix}`, adminEmail));

  await admin.getByRole('link', { name: 'Settings' }).click();
  await admin.getByLabel('Invite email').fill(teammateEmail);
  await admin.getByRole('combobox', { name: 'Role', exact: true }).selectOption('agent');
  await admin.getByRole('button', { name: 'Create invite' }).click();
  const inviteURL = await admin.getByRole('textbox', { name: 'One-time invite link' }).inputValue();
  expect(inviteURL).toMatch(new RegExp(`^${baseURL}/sign-in\\?token=`));
  const invites = admin.getByRole('region', { name: 'Invites' });
  await expect(invites.getByRole('article').filter({ hasText: teammateEmail.toLowerCase() })).toContainText('Pending');

  const teammateContext = await browser.newContext();
  const teammate = await teammateContext.newPage();
  // The ceiling is a maximum: asking for more than the invite allows is refused and keeps the link usable.
  const escalate = await teammate.request.post(`${baseURL}/api/v1/auth/redeem`, {
    data: { token: new URL(inviteURL).searchParams.get('token'), requested_role: 'admin', session_type: 'cookie' },
  });
  expect(escalate.status()).toBe(401);
  await redeemLink(teammate, inviteURL);
  await expect(appNavigationWith(teammate, teammateEmail.toLowerCase())).toHaveCount(1);
  const me = await (await teammate.request.get(`${baseURL}/api/v1/auth/me`)).json() as { agent: { role: string; email: string } };
  expect(me.agent).toMatchObject({ role: 'agent', email: teammateEmail.toLowerCase() });

  // Agents work conversations but cannot invite, manage the email channel or change the workspace timezone.
  await teammate.getByRole('link', { name: 'Settings' }).click();
  await expect(teammate.getByRole('heading', { name: 'Settings' })).toBeVisible();
  await expect(teammate.getByRole('button', { name: 'Create invite' })).toHaveCount(0);
  await expect(teammate.getByRole('button', { name: 'Save support address' })).toHaveCount(0);
  await expect(teammate.getByLabel('Workspace timezone')).toHaveCount(0);
  for (const [method, path, data] of [
    ['post', '/api/v1/auth/invites', { email: `x-${suffix}@example.com`, role_ceiling: 'agent' }],
    ['put', '/api/v1/channels/email', { support_address: `support-${suffix}@example.com` }],
    ['patch', '/api/v1/workspace/timezone', { timezone: 'Europe/Berlin' }],
  ] as const) {
    const response = await teammate.request[method](`${baseURL}${path}`, { headers, data });
    expect(response.status(), `${method} ${path}`).toBe(403);
  }

  // The admin sees the accepted invite and the teammate's device, then revokes the device.
  await admin.reload();
  await expect(invites.getByRole('article').filter({ hasText: teammateEmail.toLowerCase() })).toContainText('Accepted');
  const sessions = admin.getByRole('region', { name: 'Sessions' });
  const teammateSession = sessions.getByRole('article').filter({ hasText: teammateEmail.toLowerCase() });
  await expect(teammateSession).toHaveCount(1);
  await teammateSession.getByRole('button', { name: 'Revoke' }).click();
  await expect(teammateSession).toContainText('Revoked');
  await expect(teammateSession.getByRole('button', { name: 'Revoke' })).toHaveCount(0);

  expect((await teammate.request.get(`${baseURL}/api/v1/auth/me`)).status()).toBe(401);
  // Still on the page, the teammate keeps working: the first refused request signs the app out.
  await teammate.getByRole('link', { name: 'Inbox' }).click();
  await expect(teammate.getByRole('alert')).toContainText('You are signed out');
  await teammate.goto(baseURL);
  await expect(teammate.getByRole('alert')).toContainText('You are signed out');
  await expect(teammate.getByRole('alert')).toContainText('Ask your workspace admin for a new link');
  await expectReadablePage(teammate);

  // The admin's own session is unaffected.
  expect((await admin.request.get(`${baseURL}/api/v1/auth/me`)).status()).toBe(200);

  // The team list shows the teammate; deactivating asks first, ends their access and can be undone.
  const team = admin.getByRole('region', { name: 'Team' });
  const teammateRow = team.getByRole('article').filter({ hasText: teammateEmail.toLowerCase() });
  await expect(teammateRow).toContainText('Active');
  await teammateRow.getByRole('button', { name: `Deactivate ${teammateEmail.toLowerCase()}` }).click();
  await team.getByRole('button', { name: 'Yes, deactivate' }).click();
  await expect(teammateRow).toContainText('Deactivated');
  expect((await admin.request.get(`${baseURL}/api/v1/agents`)).status()).toBe(200);
  await teammateRow.getByRole('button', { name: `Reactivate ${teammateEmail.toLowerCase()}` }).click();
  await expect(teammateRow).toContainText('Active');
  await expect(teammateRow).not.toContainText('Deactivated');

  // An unused invite can be revoked; its link then signs nobody in.
  await admin.getByLabel('Invite email').fill(`late-${suffix}@example.com`);
  await admin.getByRole('button', { name: 'Create invite' }).click();
  const lateURL = await admin.getByRole('textbox', { name: 'One-time invite link' }).inputValue();
  expect(lateURL).not.toBe(inviteURL);
  const late = invites.getByRole('article').filter({ hasText: `late-${suffix}@example.com` });
  await late.getByRole('button', { name: 'Revoke' }).click();
  await expect(late).toContainText('Revoked');
  const lateContext = await browser.newContext();
  const latePage = await lateContext.newPage();
  await latePage.goto(lateURL);
  await latePage.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(latePage.getByRole('alert')).toContainText('This link is invalid, expired, or already used.');
  await expect(latePage).toHaveURL(/\/sign-in\?token=/);

  await Promise.all([adminContext.close(), teammateContext.close(), lateContext.close()]);
});
