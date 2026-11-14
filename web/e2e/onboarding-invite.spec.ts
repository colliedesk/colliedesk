import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { appNavigationWith, expectReadablePage, redeemLink } from './support/browser';

// Journey 2: docs/user/invites.md and docs/user/sessions.md with two real browsers.
const baseURL = servers.main;
const headers = { Origin: baseURL };

test('invite teammate: admin invites, teammate joins with the Agent role, admin revokes, revoked session refused', { tag: '@onboarding' }, async ({ browser }) => {
  const suffix = uniqueSuffix();
  const adminEmail = `admin-${suffix}@example.com`;
  const teammateEmail = `Sam.${suffix}@Example.com`;
  const adminContext = await browser.newContext();
  const admin = await adminContext.newPage();
  await redeemLink(admin, createWorkspace(`Invite Desk ${suffix}`, adminEmail));

  // Settings opens Account; invites are on Members, in the Invites tab.
  await admin.getByRole('link', { name: 'Settings' }).click();
  await admin.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Members' }).click();
  await admin.getByRole('tab', { name: /^Invites/ }).click();
  await admin.getByLabel('Invite email').fill(teammateEmail);
  // An invite gives the Agent role unless the admin picks other roles.
  await expect(admin.getByRole('button', { name: 'Roles: Agent' })).toBeVisible();
  await admin.getByRole('button', { name: 'Create invite' }).click();
  const inviteURL = await admin.getByRole('textbox', { name: 'One-time invite link' }).inputValue();
  expect(inviteURL).toMatch(new RegExp(`^${baseURL}/sign-in\\?token=`));
  const invites = admin.getByRole('region', { name: 'Invites' });
  await expect(invites.getByRole('article').filter({ hasText: teammateEmail.toLowerCase() })).toContainText('Pending');

  const teammateContext = await browser.newContext();
  const teammate = await teammateContext.newPage();
  // The invite gives roles; asking to sign in as more than they give is refused and keeps the link usable.
  const escalate = await teammate.request.post(`${baseURL}/api/v1/auth/redeem`, {
    data: { token: new URL(inviteURL).searchParams.get('token'), requested_role: 'admin', session_type: 'cookie' },
  });
  expect(escalate.status()).toBe(401);
  // The invite page names the workspace, who invited and the role before the link is used.
  await teammate.goto(inviteURL);
  await expect(teammate.getByRole('heading', { level: 1, name: `Join Invite Desk ${suffix}` })).toBeVisible();
  await expect(teammate.getByText(`${adminEmail} invited you to the support team.`)).toBeVisible();
  const facts = teammate.getByRole('list', { name: 'Invite' });
  await expect(facts).toContainText(teammateEmail.toLowerCase());
  await expect(facts).toContainText('Agent');
  await teammate.getByRole('button', { name: `Join Invite Desk ${suffix}` }).click();
  await expect(teammate).toHaveURL(`${baseURL}/`);
  await expect(appNavigationWith(teammate, teammateEmail.toLowerCase())).toHaveCount(1);
  const me = await (await teammate.request.get(`${baseURL}/api/v1/auth/me`)).json() as { agent: { roles: { name: string }[]; email: string }; permissions: string[] };
  expect(me.agent).toMatchObject({ roles: [{ name: 'Agent' }], email: teammateEmail.toLowerCase() });
  expect(me.permissions).not.toContain('team:invite');

  // Agents work conversations but cannot invite, manage the email channel or change the workspace timezone.
  await teammate.getByRole('link', { name: 'Settings' }).click();
  await expect(teammate.getByRole('heading', { name: 'Account', level: 1 })).toBeVisible();
  await expect(teammate.getByRole('button', { name: 'Create invite' })).toHaveCount(0);
  await expect(teammate.getByLabel('Workspace timezone')).toHaveCount(0);
  // The email channel lives under Channels; an agent reads it there and cannot change it.
  await teammate.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Channels' }).click();
  await expect(teammate.getByRole('heading', { name: 'Channels', level: 1 })).toBeVisible();
  await expect(teammate.getByRole('button', { name: 'Add channel' })).toHaveCount(0);
  await teammate.goto(`${baseURL}/settings/channels/type/email`);
  await expect(teammate.getByRole('heading', { name: 'Email', exact: true })).toBeVisible();
  await expect(teammate.getByText('Support address:')).toBeVisible();
  await expect(teammate.getByRole('button', { name: 'Save support address' })).toHaveCount(0);
  for (const [method, path, data] of [
    ['post', '/api/v1/auth/invites', { email: `x-${suffix}@example.com` }],
    ['put', '/api/v1/channels/email', { support_address: `support-${suffix}@example.com` }],
    ['patch', '/api/v1/workspace/timezone', { timezone: 'Europe/Berlin' }],
  ] as const) {
    const response = await teammate.request[method](`${baseURL}${path}`, { headers, data });
    expect(response.status(), `${method} ${path}`).toBe(403);
  }

  // The admin sees the accepted invite and the teammate's device, then revokes the device.
  await admin.reload();
  await expect(invites.getByRole('article').filter({ hasText: teammateEmail.toLowerCase() })).toContainText('Accepted');
  // The devices signed in are listed on Account.
  await admin.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Account' }).click();
  const sessions = admin.getByRole('region', { name: 'Sessions' });
  const teammateSession = sessions.getByRole('article').filter({ hasText: teammateEmail.toLowerCase() });
  await expect(teammateSession).toHaveCount(1);
  await teammateSession.getByRole('button', { name: 'Revoke' }).click();
  await expect(teammateSession).toHaveCount(0);

  expect((await teammate.request.get(`${baseURL}/api/v1/auth/me`)).status()).toBe(401);
  // Still on the page, the teammate keeps working: the first refused request signs the app out.
  await teammate.getByRole('link', { name: 'Back to app' }).click();
  await expect(teammate.getByRole('alert')).toContainText('You are signed out');
  await teammate.goto(baseURL);
  await expect(teammate.getByRole('alert')).toContainText('You are signed out');
  await expect(teammate.getByRole('alert')).toContainText('Ask your workspace admin for a new link');
  await expectReadablePage(teammate);

  // The admin's own session is unaffected.
  expect((await admin.request.get(`${baseURL}/api/v1/auth/me`)).status()).toBe(200);

  // The team list shows the teammate; deactivating asks first, ends their access and can be undone.
  await admin.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Members' }).click();
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

  // An unused invite can be revoked; its link then signs nobody in. Invites are their own tab.
  await admin.getByRole('tab', { name: /^Invites/ }).click();
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
  // The page says the invite was cancelled and whom to ask, and offers no Join.
  await expect(latePage.getByRole('heading', { level: 1, name: 'This invite no longer works' })).toBeVisible();
  await expect(latePage.getByText(`This invite to Invite Desk ${suffix} was cancelled. Ask ${adminEmail} if you still need access.`)).toBeVisible();
  await expect(latePage.getByRole('button', { name: /^Join/ })).toHaveCount(0);
  await expect(latePage).toHaveURL(/\/sign-in\?token=/);
  const lateRedeem = await latePage.request.post(`${baseURL}/api/v1/auth/redeem`, { data: { token: new URL(lateURL).searchParams.get('token'), session_type: 'cookie' } });
  expect(lateRedeem.status()).toBe(401);
  await expectReadablePage(latePage);

  await Promise.all([adminContext.close(), teammateContext.close(), lateContext.close()]);
});
