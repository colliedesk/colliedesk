import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// docs/user/roles-and-permissions.md: a person whose roles hold only part of the app opens every screen
// without one refused call. A screen they may not read says so; what they may read loads.
const narrowRoles: Record<string, string[]> = {
  'Knowledge only': ['kb_articles:read', 'kb_media:read', 'help_center:read', 'profile:update'],
  'Conversations only': ['conversations:read', 'conversations:update', 'messages:read', 'messages:note', 'profile:update'],
};

test('a narrow role opens every screen without a refused call', async ({ page, browser }) => {
  test.setTimeout(240_000);
  const suffix = uniqueSuffix();
  const base = servers.main;
  const headers = { Origin: base };
  await redeemLink(page, createWorkspace(`Narrow ${suffix}`, `admin-${suffix}@example.com`));
  const contact = await (await page.request.post(`${base}/api/v1/contacts`, { headers, data: { name: 'Customer', email: `c-${suffix}@example.com` } })).json() as { id: string };
  const conversation = await (await page.request.post(`${base}/api/v1/conversations`, { headers, data: { contact_id: contact.id, subject: 'Hello' } })).json() as { id: string };
  const screens = ['/', `/conversations/${conversation.id}`, `/contacts/${contact.id}`, '/knowledge', '/knowledge/media', '/knowledge/media/unused', '/knowledge/media/trash',
    '/results', '/review', '/fixes', '/settings', '/settings/roles', '/settings/targets', '/settings/numbers', '/settings/channels', '/settings/ai'];
  for (const [name, permissions] of Object.entries(narrowRoles)) {
    expect((await page.request.post(`${base}/api/v1/roles`, { headers: { ...headers, 'Idempotency-Key': `role-${name.length}-${suffix}` }, data: { name, permissions } })).status()).toBe(201);
    const { url } = await (await page.request.post(`${base}/api/v1/auth/invites`, { headers, data: { email: `${name.length}-${suffix}@example.com`, roles: [name] } })).json() as { url: string };
    const context = await browser.newContext();
    const narrow = await context.newPage();
    const refused: string[] = [];
    narrow.on('response', (response) => { if (response.status() === 403) refused.push(`${response.request().method()} ${new URL(response.url()).pathname}`); });
    await narrow.goto(url);
    await narrow.getByRole('button', { name: 'Continue to Colliedesk' }).click();
    await narrow.waitForLoadState('networkidle');
    for (const screen of screens) {
      await narrow.goto(`${base}${screen}`);
      await narrow.waitForLoadState('networkidle');
      expect(refused, `${name} on ${screen}`).toEqual([]);
    }
    // The inbox address takes a role that may not read conversations to an area it may open.
    if (!permissions.includes('conversations:read')) {
      await narrow.goto(`${base}/`);
      await expect(narrow).toHaveURL(`${base}/knowledge`);
    }
    await context.close();
  }
});
