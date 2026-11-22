import { readFileSync } from 'node:fs';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

// A 180 × 180 PNG: big enough to draw on.
const picture = readFileSync(path.resolve(import.meta.dirname, '../public/apple-touch-icon.png'));
const imageID = /\/api\/v1\/kb\/images\/([0-9a-f-]{36})/;

// signIn opens a new workspace with a collection and an image in its library.
async function signIn(page: Page, name: string) {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`${name} ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  const { id: collection } = await (await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Guides' } })).json() as { id: string };
  const upload = await page.request.post(`${servers.main}/api/v1/kb/images?name=${encodeURIComponent('Settings page.png')}`,
    { headers: { ...headers, 'Content-Type': 'image/png' }, data: picture });
  expect(upload.status()).toBe(201);
  const { id: image } = await upload.json() as { id: string };
  await page.setViewportSize({ width: 1600, height: 1000 });
  return { headers, collection, image };
}

// docs/user/knowledge-base.md "Annotate an image": the annotated image is a new library item that took the
// original's place in the article; the original stays as its earlier version, and Undo puts it back.
test('knowledge base: an image annotated with a box is saved as a copy in the article, the library keeps the original as its earlier version, and Undo puts it back', async ({ page }) => {
  const { headers, collection, image } = await signIn(page, 'KB Annotate');
  const { id: article } = await (await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers,
    data: { collection_id: collection, title: 'Find the settings', body_md: `Open the page.\n\n![Settings](/api/v1/kb/images/${image})\n` } })).json() as { id: string };
  const body = async () => (await (await page.request.get(`${servers.main}/api/v1/kb/articles/${article}`)).json() as { body_md: string }).body_md;

  await page.goto(`${servers.main}/knowledge/articles/${article}`);
  await page.getByRole('textbox', { name: 'Text', exact: true }).getByRole('img', { name: 'Settings' }).click();
  await page.getByRole('toolbar', { name: 'Image' }).getByRole('button', { name: 'Annotate' }).click();
  const dialog = page.getByRole('dialog', { name: 'Annotate image' });
  const save = dialog.getByRole('button', { name: 'Save copy' });
  await expect(save).toBeDisabled();
  await dialog.getByRole('button', { name: 'Box' }).click();
  await expect(dialog.getByRole('button', { name: 'Box' })).toHaveAttribute('aria-pressed', 'true');
  const canvas = dialog.locator('canvas');
  await expect(canvas).toHaveJSProperty('width', 180);
  const box = (await canvas.boundingBox())!;
  await page.mouse.move(box.x + box.width * 0.2, box.y + box.height * 0.2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * 0.7, box.y + box.height * 0.6, { steps: 5 });
  await page.mouse.up();
  await save.click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText('Annotated. The original stays in the media library as an earlier version.')).toBeVisible();

  // The article now shows the copy; the copy is a new item.
  const copy = (await body()).match(imageID)![1]!;
  expect(copy).not.toBe(image);
  expect(await body()).toBe(`Open the page.\n\n![Settings](/api/v1/kb/images/${copy})\n`);
  const bytes = await (await page.request.get(`${servers.main}/api/v1/kb/images/${copy}`)).body();
  expect(bytes.equals(picture)).toBe(false);

  await page.goto(`${servers.main}/knowledge/media?item=${copy}`);
  const panel = page.getByRole('complementary', { name: 'Settings page (annotated).png' });
  const earlier = panel.getByRole('region', { name: 'Earlier version' });
  await expect(earlier).toContainText('Settings page.png');
  await expect(earlier).toContainText('Replaced this in 1 article.');
  await earlier.getByRole('button', { name: 'Undo' }).click();
  await expect(page.getByText('Put back.')).toBeVisible();
  expect(await body()).toBe(`Open the page.\n\n![Settings](/api/v1/kb/images/${image})\n`);
});

// docs/user/knowledge-base.md "Image size and position": the width and alignment set on the image bar reach the
// help center as its own classes, with no style attribute.
test('knowledge base: an image set to 480 pixels and centred in the editor is published and the help center shows it that way', async ({ page }) => {
  const { headers, collection, image } = await signIn(page, 'KB Figure');
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Figure help', locale: 'en' } })).status()).toBe(200);
  const article = await (await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers,
    data: { collection_id: collection, title: 'Find the settings', body_md: `Open the page.\n\n![Settings](/api/v1/kb/images/${image})\n`, status: 'published' } })).json() as { id: string; url: string };

  await page.goto(`${servers.main}/knowledge/articles/${article.id}`);
  await page.getByRole('textbox', { name: 'Text', exact: true }).getByRole('img', { name: 'Settings' }).click();
  const bar = page.getByRole('toolbar', { name: 'Image' });
  await bar.getByRole('combobox', { name: 'Width' }).selectOption({ label: 'Pixels…' });
  await bar.getByRole('spinbutton', { name: 'Width in pixels' }).fill('480');
  await bar.getByRole('combobox', { name: 'Alignment' }).selectOption({ label: 'Centre' });
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('group', { name: 'Unpublished changes' })).toBeVisible();
  await page.getByRole('button', { name: 'Publish changes' }).click();
  await expect(page.getByRole('group', { name: 'Unpublished changes' })).toHaveCount(0);

  await page.goto(article.url);
  const figure = page.locator('.prose figure');
  await expect(figure).toHaveClass('kb-figure kb-figure-center');
  await expect(figure.locator('img')).toHaveAttribute('width', '480');
  await expect(figure.locator('img')).toHaveJSProperty('offsetWidth', 480);
  expect(await page.locator('.prose [style]').count()).toBe(0);
});
