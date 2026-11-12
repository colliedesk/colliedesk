import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// A permission the person does not hold is a dashed chip with a lock; the box under it never shows, also
// where the page dims its disabled controls (Settings → External AI agents).
test('a locked permission chip shows its label, not the box under it', async ({ page, browser }) => {
  const suffix = uniqueSuffix();
  const base = servers.main;
  await redeemLink(page, createWorkspace(`Locked ${suffix}`, `admin-${suffix}@example.com`));
  const { url } = await (await page.request.post(`${base}/api/v1/auth/invites`, { headers: { Origin: base }, data: { email: `a-${suffix}@example.com` } })).json() as { url: string };
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const agent = await context.newPage();
  await redeemLink(agent, url);
  await agent.goto(`${base}/settings#agents-heading`);
  // The form is in the dialog Create token opens.
  await agent.getByRole('region', { name: 'External AI agents' }).getByRole('button', { name: 'Create token' }).click();
  await agent.getByRole('radio', { name: 'Pick permissions (fixed)' }).check();
  for (const name of [/Team and sessions: Invite/, /Team and sessions: View/]) {
    await expect(agent.getByRole('checkbox', { name })).toHaveCSS('opacity', '0');
  }
  await expect(agent.getByRole('checkbox', { name: /Team and sessions: Invite/ })).toHaveAttribute('aria-disabled', 'true');
  await context.close();
});
