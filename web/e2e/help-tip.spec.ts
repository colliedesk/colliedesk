import { expect, test, type Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// docs/internals/design.md › Quiet by default: a locked control says why in the tip of its lock. On a phone the tip
// opens by tap, stays open after the finger lifts, sits 8 px inside the screen, hides while its lock is scrolled
// away, stays open on a tap on its own text and closes with a tap elsewhere. Escape closes the tip only, never the editor around it.
const reason = 'The only active member with the Admin role keeps it. Give Admin to someone else first.';

// openOwnRoles signs in as the only admin of a new workspace and opens Change roles on themself, where Admin is locked.
async function openOwnRoles(page: Page, touch: boolean) {
  const suffix = `${Date.now()}-${process.pid}`;
  const email = `owner-${suffix}@example.com`;
  await redeemLink(page, createWorkspaceWithSlug(`Help Tip ${suffix}`, email).url);
  await page.goto(`${servers.main}/settings/members`);
  const team = page.getByRole('region', { name: 'Team' });
  const change = team.getByRole('button', { name: `Change roles: ${email}` });
  await (touch ? change.tap() : change.click());
  return { team, editor: team.getByRole('group', { name: /^Roles of / }), lock: team.getByRole('button', { name: 'Why Admin is locked' }) };
}

test.describe('on a 390 px touch screen', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('a tap opens a lock tip 8 px inside the screen; it hides scrolled away, stays on a tap on its text, and a tap elsewhere closes it', async ({ page }) => {
    const { team, lock } = await openOwnRoles(page, true);
    await expect(team.getByText(reason)).toBeHidden();
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
    expect(box!.x).toBeGreaterThanOrEqual(8);
    expect(box!.y).toBeGreaterThanOrEqual(8);
    expect(box!.x + box!.width).toBeLessThanOrEqual(390 - 8);
    expect(box!.y + box!.height).toBeLessThanOrEqual(844 - 8);

    // The panel that scrolls the lock moves it out of sight; the tip must not stay pinned to the screen's edge.
    // The People tab is shorter than the screen is tall, so the test gives the page length to scroll.
    await page.evaluate(() => { const spacer = document.createElement('div'); spacer.style.height = '4000px'; document.querySelector('.settings-sections')?.append(spacer); });
    const scroll = (by: number) => lock.evaluate((element, distance) => {
      let at = element.parentElement;
      while (at && !(at.scrollHeight > at.clientHeight && /auto|scroll/.test(getComputedStyle(at).overflowY))) at = at.parentElement;
      (at ?? document.scrollingElement!).scrollBy(0, distance);
    }, by);
    await scroll(3000);
    await expect(lock).not.toBeInViewport();
    await expect(tip).toBeHidden();
    await scroll(-3000);
    await lock.scrollIntoViewIfNeeded();
    await expect(tip).toBeVisible();

    await tip.tap();
    await page.waitForTimeout(300);
    await expect(tip).toBeVisible();

    await team.getByRole('heading', { name: 'Team' }).tap();
    await expect(tip).toBeHidden();
  });
});

test('with a mouse, Escape on a lock tip closes the tip only: Change roles stays open with its picks', async ({ page }) => {
  const { editor, lock } = await openOwnRoles(page, false);
  const agent = editor.getByRole('checkbox', { name: /Agent/ });
  await agent.check();
  await lock.hover();
  await expect(page.getByRole('tooltip')).toHaveText(reason);
  await agent.focus();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('tooltip')).toBeHidden();
  await expect(editor).toBeVisible();
  await expect(agent).toBeChecked();
  // With no tip open, Escape cancels the editor as before.
  await page.keyboard.press('Escape');
  await expect(editor).toBeHidden();
});
