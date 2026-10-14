import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

// redeemLink opens a one-time link the way a person does: open it, select Continue, land in the inbox.
export async function redeemLink(page: Page, url: string): Promise<void> {
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to CollieDesk' }).click();
  await expect(page).toHaveURL(`${new URL(url).origin}/`);
}

// expectNoRawJSON fails when the page shows an API error body instead of a sentence.
export async function expectReadablePage(page: Page): Promise<void> {
  const text = (await page.locator('body').innerText()).trim();
  expect(text.length, 'page is blank').toBeGreaterThan(0);
  expect(text).not.toMatch(/^\s*[{[]/);
  expect(text).not.toMatch(/"(code|message|error)"\s*:/);
}

// appNavigationWith finds a navigation landmark showing the text (workspace name, signed-in address).
// It does not depend on the landmark's label, which the design may change.
export function appNavigationWith(page: Page, text: string) {
  return page.getByRole('navigation').filter({ hasText: text });
}
