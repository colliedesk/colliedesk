import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

const baseURL = servers.main;

// A link to a page of the app is a plain <a href>. Clicking it changes the page without loading the
// document again, which the marker set on window shows: a reload would drop it.
test('a plain link to a page of the app opens it without reloading the document', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`E2E in-app links ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${baseURL}/`);

  const markDocument = () => page.evaluate(() => { (window as unknown as { __inAppMarker?: number }).__inAppMarker = 1; });
  const documentMarker = () => page.evaluate(() => (window as unknown as { __inAppMarker?: number }).__inAppMarker);

  await page.goto(`${baseURL}/settings/channels`);
  await expect(page.getByRole('heading', { name: 'No channel yet' })).toBeVisible();
  await markDocument();
  await page.getByRole('link', { name: /Add a web form/ }).click();
  await expect(page).toHaveURL(`${baseURL}/settings/channels/new/web_form`);
  await expect(page.getByRole('heading', { name: 'Web form', exact: true })).toBeVisible();
  expect(await documentMarker()).toBe(1);

  await page.goto(`${baseURL}/knowledge`);
  await expect(page.getByRole('heading', { level: 1, name: 'Knowledge base' })).toBeVisible();
  await markDocument();
  await page.getByRole('link', { name: 'Media', exact: true }).click();
  await expect(page).toHaveURL(`${baseURL}/knowledge/media`);
  expect(await documentMarker()).toBe(1);
});
