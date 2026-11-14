import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// A new admin on a fresh workspace, signed in, with the email channel set up when asked.
async function signIn(page: Page, name: string, options: { email?: boolean } = {}): Promise<string> {
  const suffix = `${Date.now()}-${process.pid}`;
  await redeemLink(page, createWorkspaceWithSlug(`${name} ${suffix}`, `owner-${suffix}@example.com`).url);
  if (options.email) {
    const headers = { Origin: servers.main };
    expect((await page.request.put(`${servers.main}/api/v1/channels/email`, { headers, data: { support_address: `support-${suffix}@example.com` } })).status()).toBe(200);
  }
  return suffix;
}

test('an empty inbox offers the first setup step as one button that opens it', async ({ page }) => {
  await signIn(page, 'Empty Desk');
  await page.goto(`${servers.main}/`);
  const empty = page.getByRole('region', { name: 'Get started' });
  const next = empty.getByRole('link', { name: 'Connect your support email' });
  await expect(next).toBeVisible();
  await next.click();
  await expect(page).toHaveURL(/\/setup\/email$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Receive customer email', exact: true })).toBeVisible();
});

// What a person sees of a dialog's head: the title and a field's label.
async function lookOf(dialog: Locator, label?: string) {
  const title = await dialog.getByRole('heading', { level: 2 }).evaluate((node) => {
    const style = getComputedStyle(node);
    return { size: style.fontSize, weight: style.fontWeight, line: style.lineHeight, color: style.color };
  });
  const field = label ? await dialog.locator('label', { hasText: label }).first().evaluate((node) => {
    const style = getComputedStyle(node);
    return { size: style.fontSize, weight: style.fontWeight, color: style.color };
  }) : undefined;
  return { title, field };
}

test('Start a conversation and Add channel share one title and one label style', async ({ page }) => {
  await signIn(page, 'Dialog Desk', { email: true });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`${servers.main}/`);
  await page.getByRole('button', { name: 'New conversation' }).click();
  const start = page.getByRole('dialog', { name: 'Start a conversation' });
  await expect(start).toBeVisible();
  const started = await lookOf(start, 'Subject');
  await start.getByRole('button', { name: 'Cancel' }).click();

  await page.goto(`${servers.main}/settings/channels`);
  await page.getByRole('button', { name: 'Add channel' }).click();
  const add = page.getByRole('dialog', { name: 'Add channel' });
  await expect(add).toBeVisible();
  const added = await lookOf(add);

  // The drawn title of Add a channel: 16 px, semibold, 24 px line, the main text colour.
  for (const look of [started, added]) {
    expect(look.title).toEqual({ size: '16px', weight: '600', line: '24px', color: 'rgb(23, 23, 23)' });
  }
  // The drawn label: 13 px, medium, the second text colour.
  expect(started.field).toEqual({ size: '13px', weight: '500', color: 'rgb(77, 77, 77)' });
});

test('the AI draft control keeps its name and state in words on a narrow reply box, and every tool has a tooltip', async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const suffix = await signIn(page, 'Tools Desk', { email: true });
  const headers = { Origin: servers.main };
  const contact = await page.request.post(`${servers.main}/api/v1/contacts`, { headers, data: { name: 'Tools Customer', email: `customer-${suffix}@example.com` } });
  const { id } = await contact.json() as { id: string };
  expect((await page.request.post(`${servers.main}/api/v1/conversations`, { headers, data: { contact_id: id, subject: 'Tools ticket' } })).status()).toBe(201);
  await page.goto(`${servers.main}/`);
  await page.getByRole('button', { name: /Tools ticket/ }).click();
  const tools = page.locator('.composer-actions');
  await expect(tools.getByText('Draft with AI')).toBeVisible();
  await expect(tools.getByRole('link', { name: 'Draft with AI (off)' })).toHaveAttribute('data-tip', /.+/);
  for (const name of ['Add attachment', 'Insert saved reply', 'Articles']) {
    await expect(tools.getByRole('button', { name: new RegExp(`^${name}`) })).toHaveAttribute('data-tip', /.+/);
  }
});
