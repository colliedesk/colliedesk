import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

// A screen that refreshes on a timer (a fix's status every 15 seconds, the inbox every 20) shows a change
// only on its next refresh. untilShown moves the page's clock ahead while it waits, so that refresh runs
// now instead of on the wall clock; the check itself is unchanged. The page must install its clock
// (page.clock.install()) before it loads.
export async function untilShown(page: Page, check: () => Promise<unknown>, timeout = 30_000): Promise<void> {
  await expect(async () => {
    await page.clock.fastForward(5_000);
    await check();
  }).toPass({ timeout, intervals: [250] });
}
