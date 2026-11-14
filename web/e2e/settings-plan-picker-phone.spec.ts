import { expect, test } from '@playwright/test';
import { servers, uniqueSuffix } from './support/cli';
import { IdentityProvider } from './support/identity-provider';
import { signUp } from './support/signup';

// M-S3: on a phone the plans are stacked, the plan that fits leads, and every choice is a touch target inside the
// screen. The plan picker is offered only by the cloud server of the suite, whose billing stand-in offers the Founding
// plan alone for a trial, so the stack is one card tall here.
const baseURL = servers.cloud;
const provider = new IdentityProvider();

test.beforeAll(async () => { await provider.start(); });
test.afterAll(async () => { await provider.stop(); });

test('on a phone the plan card fills the screen, leads, and its interval choice and Choose button are touch targets', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const suffix = uniqueSuffix();
  expect((await signUp(page, provider, `owner-${suffix}@example.com`, `Plan phone ${suffix}`)).status()).toBe(201);
  await page.goto(`${baseURL}/settings/billing`);
  const picker = page.getByRole('region', { name: /upgrade|plan/i }).filter({ has: page.locator('.plan-card') }).first();
  await expect(picker).toBeVisible();
  const cards = picker.locator('article.plan-card');
  const count = await cards.count();
  expect(count, 'the trial offers a plan').toBeGreaterThanOrEqual(1);
  const boxes = await Promise.all((await cards.all()).map((card) => card.boundingBox()));
  let bottom = 0;
  for (const box of boxes) {
    expect(box, 'every plan is drawn').not.toBeNull();
    expect(box!.x, 'inside the screen').toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width, 'inside the screen').toBeLessThanOrEqual(390);
    expect(box!.width, 'a stacked card takes the width of the screen').toBeGreaterThanOrEqual(320);
    expect(box!.y, 'stacked: each plan starts below the one before').toBeGreaterThanOrEqual(bottom - 1);
    bottom = box!.y + box!.height;
  }
  // The plan that fits the team leads: the first card carries the word, and it is the only one that does.
  await expect(cards.first()).toHaveClass(/plan-card-picked/);
  await expect(picker.locator('.plan-card-picked')).toHaveCount(1);
  for (const choose of await picker.getByRole('button', { name: /^Choose / }).all()) {
    await choose.scrollIntoViewIfNeeded();
    const box = (await choose.boundingBox())!;
    expect(box.height, 'a touch target').toBeGreaterThanOrEqual(44);
    expect(box.x + box.width, 'inside the screen').toBeLessThanOrEqual(390);
  }
  for (const label of await picker.locator('fieldset.segmented label').all()) {
    const box = (await label.boundingBox())!;
    expect(box.height, 'a touch target').toBeGreaterThanOrEqual(44);
    expect(box.x + box.width, 'inside the screen').toBeLessThanOrEqual(390);
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'no sideways scroll').toBe(true);
});
