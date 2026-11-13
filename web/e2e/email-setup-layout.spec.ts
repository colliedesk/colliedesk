import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';

// The setup checklist on the inbox ("Finish setting up") and the email setup on Settings → Email both list steps.
// Opened one after the other in the same page, each keeps its own layout: an email step's head (number, title,
// "Step 1 of 4") runs across the step, its title on one line, and the step's fields sit under it.
test.use({ viewport: { width: 1440, height: 900 } });

test('email setup steps keep their layout after the inbox has shown the setup checklist', async ({ page }) => {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Layout Desk ${suffix}`, `admin-${suffix}@acme.example`));
  await expect(page.getByText('Finish setting up').first()).toBeVisible();

  await page.getByRole('link', { name: 'Settings' }).click();
  await page.getByRole('navigation', { name: 'Settings sections' }).getByRole('link', { name: 'Email', exact: true }).click();
  const address = page.getByRole('group', { name: 'Your support address' });
  await expect(address).toContainText('Step 1 of 4');

  const layout = await address.evaluate((step) => {
    const head = step.querySelector('.setup-step-head')!.getBoundingClientRect();
    const title = step.querySelector('.setup-step-head h3')!.getBoundingClientRect();
    const field = step.querySelector('input')!.getBoundingClientRect();
    const box = step.getBoundingClientRect();
    return { headShare: head.width / box.width, titleHeight: title.height, fieldBelowHead: field.top >= head.bottom };
  });
  expect.soft(layout.headShare, 'the step head runs across the step').toBeGreaterThan(0.8);
  expect.soft(layout.titleHeight, 'the step title stays on one line').toBeLessThan(32);
  expect.soft(layout.fieldBelowHead, 'the fields sit under the head').toBe(true);
  await page.screenshot({ path: test.info().outputPath('email-setup.png') });
});
