import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

// redeemLink opens a one-time link the way a person does: open it, select Continue, land in the inbox.
export async function redeemLink(page: Page, url: string): Promise<void> {
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${new URL(url).origin}/`);
}

// expectNoRawJSON fails when the page shows an API error body instead of a sentence.
export async function expectReadablePage(page: Page): Promise<void> {
  const text = (await page.locator('body').innerText()).trim();
  expect(text.length, 'page is blank').toBeGreaterThan(0);
  expect(text).not.toMatch(/^\s*[{[]/);
  expect(text).not.toMatch(/"(code|message|error)"\s*:/);
}

// appNavigationWith finds the bar that shows the workspace name and the signed-in address. It
// does not depend on how the design lays the bar out, only on its landmark name.
export function appNavigationWith(page: Page, text: string) {
  return page.getByRole('region', { name: 'Signed in as' }).filter({ hasText: text });
}

// expectIdentityVisible proves a person can read the workspace and address. Playwright counts text
// with opacity 0 as visible, so this also checks opacity, clipping and size.
export async function expectIdentityVisible(page: Page, ...texts: string[]): Promise<void> {
  const bar = page.getByRole('region', { name: 'Signed in as' });
  await expect(bar).toBeVisible();
  for (const text of texts) {
    const part = bar.getByText(text, { exact: true });
    await expect(part).toBeVisible();
    await expect(part).toHaveCSS('opacity', '1');
    const readable = await part.evaluate((element) => {
      const box = element.getBoundingClientRect();
      return element.checkVisibility({ opacityProperty: true, visibilityProperty: true }) && box.width > 8 && box.height > 8
        && box.right <= window.innerWidth && box.left >= 0;
    });
    expect(readable, `${text} is readable on screen`).toBe(true);
  }
}
