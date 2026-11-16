import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// A link to a conversation or customer that is not there (a wrong id, something that is not an id) says it was
// not found and leads back to the inbox. It offers no Retry: the server would answer 404 again.
for (const viewport of [{ width: 1365, height: 900 }, { width: 390, height: 844 }]) {
  test(`a link to a missing conversation or customer says not found and leads back to the inbox (${viewport.width}px)`, async ({ page }) => {
    const suffix = `${Date.now()}-${process.pid}-${viewport.width}`;
    await page.setViewportSize(viewport);
    await redeemLink(page, createWorkspaceWithSlug(`E2E link not found ${suffix}`, `admin-${suffix}@example.com`).url);

    for (const id of ['00000000-0000-0000-0000-000000000000', 'abc']) {
      await page.goto(`${servers.main}/conversations/${id}`);
      const timeline = page.getByRole('region', { name: 'Timeline' });
      await expect(timeline.getByRole('heading', { name: 'Conversation not found' })).toBeVisible();
      await expect(timeline).toContainText('The link may be wrong, or the conversation was deleted or merged, or you cannot see it.');
      await expect(page.getByRole('button', { name: 'Retry' })).toHaveCount(0);
      await expect(page.getByText('This conversation could not be loaded.')).toHaveCount(0);
      await expect(page.getByRole('button', { name: 'Send reply' })).toHaveCount(0);
    }
    await page.getByRole('link', { name: 'Back to Inbox' }).click();
    await expect(page).toHaveURL(`${servers.main}/`);

    await page.goto(`${servers.main}/contacts/00000000-0000-0000-0000-000000000000`);
    await expect(page.getByRole('heading', { name: 'Customer not found' })).toBeVisible();
    await expect(page.getByText('This customer could not be loaded.')).toHaveCount(0);
    await page.getByRole('link', { name: 'Back to Inbox' }).click();
    await expect(page).toHaveURL(`${servers.main}/`);
  });
}
