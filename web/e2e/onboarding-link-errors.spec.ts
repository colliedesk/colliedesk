import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { createWorkspace, psql, servers, uniqueSuffix } from './support/cli';
import { appNavigationWith, expectReadablePage, redeemLink } from './support/browser';

// Journey 6: the link problems a new customer actually hits, each answered with a sentence and a next step.
const baseURL = servers.main;
const invalid = 'This link is invalid, expired, or already used.';
const nextStep = 'Ask your workspace admin for a new link.';

async function openAndContinue(page: Page, url: string) {
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
}

async function expectLinkRefused(page: Page) {
  await expect(page.getByRole('alert')).toContainText(invalid);
  await expect(page.getByRole('alert')).toContainText(nextStep);
  await expect(page).toHaveURL(/\/sign-in\?token=/);
  await expectReadablePage(page);
}

test('link errors: expired, reused, revoked, mistyped, missing and another workspace\'s link explain themselves', { tag: '@onboarding' }, async ({ browser }) => {
  const suffix = uniqueSuffix();

  const expiredURL = createWorkspace(`Expired Desk ${suffix}`, `expired-${suffix}@example.com`);
  // Fifteen minutes pass: the link row is aged in the database, nothing in the product is stubbed.
  psql(`UPDATE pairing_links SET expires_at = now() - interval '1 second' WHERE email = 'expired-${suffix}@example.com'`);
  const expiredContext = await browser.newContext();
  const expiredPage = await expiredContext.newPage();
  await openAndContinue(expiredPage, expiredURL);
  await expectLinkRefused(expiredPage);

  const reusedURL = createWorkspace(`Reused Desk ${suffix}`, `reused-${suffix}@example.com`);
  const first = await browser.newContext();
  await redeemLink(await first.newPage(), reusedURL);
  const second = await browser.newContext();
  const secondPage = await second.newPage();
  await openAndContinue(secondPage, reusedURL);
  await expectLinkRefused(secondPage);

  const mistypedPage = await (await browser.newContext()).newPage();
  await openAndContinue(mistypedPage, `${reusedURL.slice(0, -4)}xxxx`);
  await expectLinkRefused(mistypedPage);

  // A bare /sign-in (link cut off by a mail client) says what to do instead of showing an empty form.
  const bare = await (await browser.newContext()).newPage();
  await bare.goto(`${baseURL}/sign-in`);
  await expect(bare.getByText('Ask your workspace admin for a pairing or invite link.')).toBeVisible();
  await expectReadablePage(bare);

  // Signed in to workspace A, the person opens a link for workspace B. The app must say which
  // workspace the browser is now in, so nobody answers B's customers believing they are in A.
  const alpha = await browser.newContext();
  const alphaPage = await alpha.newPage();
  await redeemLink(alphaPage, createWorkspace(`Alpha Desk ${suffix}`, `alpha-${suffix}@example.com`));
  await expect(appNavigationWith(alphaPage, `Alpha Desk ${suffix}`)).toHaveCount(1);
  const alphaContact = await (await alphaPage.request.post(`${baseURL}/api/v1/contacts`, { headers: { Origin: baseURL }, data: { name: 'Alpha customer', email: `alpha-customer-${suffix}@example.com` } })).json() as { id: string };
  await alphaPage.request.post(`${baseURL}/api/v1/conversations`, { headers: { Origin: baseURL }, data: { contact_id: alphaContact.id, subject: `Alpha only ${suffix}` } });
  await alphaPage.reload();
  await expect(alphaPage.getByRole('button', { name: new RegExp(`Alpha only ${suffix}`) })).toBeVisible();
  await expect(alphaPage.getByRole('option', { name: `alpha-${suffix}@example.com` })).toHaveCount(1);
  await openAndContinue(alphaPage, createWorkspace(`Beta Desk ${suffix}`, `beta-${suffix}@example.com`));
  await expect(alphaPage).toHaveURL(`${baseURL}/`);
  await expect(appNavigationWith(alphaPage, `Beta Desk ${suffix}`)).toHaveCount(1);
  await expect(appNavigationWith(alphaPage, `beta-${suffix}@example.com`)).toHaveCount(1);
  await expect(appNavigationWith(alphaPage, `Alpha Desk ${suffix}`)).toHaveCount(0);
  // Nothing cached from workspace A may show inside B: not its conversations, not its agents.
  await expect(alphaPage.getByRole('button', { name: new RegExp(`Alpha only ${suffix}`) })).toHaveCount(0);
  await expect(alphaPage.getByRole('option', { name: `beta-${suffix}@example.com` })).toHaveCount(1);
  await expect(alphaPage.getByRole('option', { name: `alpha-${suffix}@example.com` })).toHaveCount(0);

  // A visitor with no session who opens the app sees how to get in, not an empty inbox.
  const stranger = await (await browser.newContext()).newPage();
  await stranger.goto(baseURL);
  await expect(stranger.getByRole('alert')).toContainText('You are signed out');
  await expect(stranger.getByRole('alert')).toContainText(nextStep);
  await expect(stranger.getByRole('button', { name: 'Send reply' })).toHaveCount(0);
  await expect(stranger.getByText(/collie pair --workspace/)).toBeVisible();
  await expect(stranger.getByRole('link', { name: 'Sign in' })).toHaveCount(0);
  await expectReadablePage(stranger);
  await stranger.goto(`${baseURL}/settings`);
  await expect(stranger.getByRole('alert')).toContainText('You are signed out');
});
