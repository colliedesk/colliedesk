import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

test('settings of a second help center: an admin opens it from the list, saves its brand colour, and only its public pages wear it', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url, slug } = createWorkspaceWithSlug(`e2e-help-center-settings-by-id ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Acme help', locale: 'en' } })).status()).toBe(200);
  expect((await page.request.post(`${servers.main}/api/v1/help-centers`, { headers, data: { name: 'Hardware', handle: 'hardware', locale: 'en', enabled: true } })).status()).toBe(201);
  const base = `${servers.main}/help/${slug}`;

  // From the list of help centers to the settings of Hardware, and its brand saved there.
  await page.goto(`${servers.main}/knowledge/settings`);
  const list = page.getByRole('list', { name: 'Help centers' });
  await list.getByRole('link', { name: 'Settings of Hardware' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Hardware' })).toBeVisible();
  const brand = page.getByRole('region', { name: 'Brand' });
  await brand.getByRole('textbox', { name: 'Primary color', exact: true }).fill('#7a1f5c');
  await brand.getByRole('button', { name: 'Save brand' }).click();
  // A page links its brand from a sheet of its own; this reads the sheet the page links now.
  const brandSheet = async (address: string) => {
    const html = await (await page.request.get(address)).text();
    const href = /href="([^"]*\/_\/brand-[^"]+\.css)"/.exec(html)?.[1];
    return href ? (await page.request.get(`${servers.main}${href}`)).text() : '';
  };
  await expect.poll(() => brandSheet(`${base}/hardware`)).toMatch(/#7a1f5c/i);

  // Hardware's pages wear the colour; the primary's keep their own.
  await page.goto(`${base}/hardware`);
  await expect(page.getByRole('button', { name: 'Search', exact: true })).toHaveCSS('background-color', 'rgb(122, 31, 92)');
  expect(await brandSheet(base)).not.toMatch(/#7a1f5c/i);
  await page.goto(base);
  await expect(page.getByRole('button', { name: 'Search', exact: true })).not.toHaveCSS('background-color', 'rgb(122, 31, 92)');

  // The primary's own Brand still reads its own colour, not the one saved on Hardware.
  await page.goto(`${servers.main}/knowledge/settings`);
  await expect(page.getByRole('region', { name: 'Brand' }).getByRole('textbox', { name: 'Primary color', exact: true })).not.toHaveValue('#7a1f5c');
});
