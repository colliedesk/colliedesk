import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// docs/internals/design.md › Quiet by default: a locked control says why in the tip of its lock. On a phone the tip
// opens by tap, stays open after the finger lifts, fits inside the screen and closes with a tap elsewhere.
test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

test('on a 390 px touch screen a tap opens a lock tip that stays on screen, and a tap elsewhere closes it', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const email = `owner-${suffix}@example.com`;
  await redeemLink(page, createWorkspaceWithSlug(`Help Tip ${suffix}`, email).url);
  await page.goto(`${servers.main}/settings`);
  const team = page.getByRole('region', { name: 'Team' });
  // The only admin of a new workspace cannot take Admin from themself; the reason is the lock's tip, not a line.
  await team.getByRole('button', { name: `Change roles: ${email}` }).tap();
  const reason = 'The only active member with the Admin role keeps it. Give Admin to someone else first.';
  await expect(team.getByText(reason)).toBeHidden();
  const lock = team.getByRole('button', { name: 'Why Admin is locked' });
  await expect(lock).toHaveAccessibleDescription(reason);
  const target = await lock.boundingBox();
  expect(target && Math.min(target.width, target.height)).toBeGreaterThanOrEqual(44);

  await lock.tap();
  const tip = page.getByRole('tooltip');
  await expect(tip).toHaveText(reason);
  await page.waitForTimeout(300);
  await expect(tip).toBeVisible();
  const box = await tip.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.y).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(390);
  expect(box!.y + box!.height).toBeLessThanOrEqual(844);

  await team.getByRole('heading', { name: 'Team' }).tap();
  await expect(tip).toBeHidden();
});
