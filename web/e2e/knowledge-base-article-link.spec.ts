import { expect, test } from '@playwright/test';
import { createWorkspace, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { writeInMarkdown } from './support/kb-editor';

// docs/user/knowledge-base.md "Link another article from the editor": two actions, Link to article and
// the article, and the Markdown holds the relative address, which works on every host of the help center.
const baseURL = servers.main;

test('knowledge base: Link to article in the editor writes a relative link to another published article, and not to the one being edited', async ({ page }) => {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Editor Links ${suffix}`, `admin-${suffix}@acme.example`, baseURL));
  const headers = { Origin: baseURL };
  const collection = await page.request.post(`${baseURL}/api/v1/kb/collections`, { headers, data: { name: 'Guides' } });
  expect(collection.status()).toBe(201);
  const collectionId = (await collection.json() as { id: string }).id;
  const make = async (title: string, body_md: string) => {
    const response = await page.request.post(`${baseURL}/api/v1/kb/articles`, { headers, data: { collection_id: collectionId, title, body_md, status: 'published' } });
    expect(response.status()).toBe(201);
    return await response.json() as { id: string; url: string };
  };
  const target = await make(`Reset your password ${suffix}`, 'Open Settings.');
  const editing = await make(`Billing basics ${suffix}`, 'To change it, ');
  const address = new URL(target.url).pathname.slice(new URL(target.url).pathname.lastIndexOf('/a/'));
  expect(address).toMatch(/^\/a\/\d+-reset-your-password-/);
  const on = await page.request.put(`${baseURL}/api/v1/help-center`, { headers, data: { enabled: true, title: '', locale: 'en' } });
  expect(on.status()).toBe(200);

  await page.goto(`${baseURL}/knowledge/articles/${editing.id}`);
  await writeInMarkdown(page);
  // Below 720px of editor the toolbar keeps this tool under More formatting (one action more); the sidebar and
  // the article's side panel leave that much only on a wide screen, in the Write tab.
  await page.setViewportSize({ width: 1800, height: 900 });
  await page.getByRole('tab', { name: 'Write' }).click();
  const text = page.getByRole('textbox', { name: 'Text (Markdown)' });
  await text.click();
  await page.keyboard.press('ControlOrMeta+End');

  // Two actions: the button, then the article.
  const toolbar = page.getByRole('toolbar', { name: 'Formatting' });
  const textTop = (await text.boundingBox())!.y;
  const button = page.getByRole('button', { name: 'Link to article' });
  const buttonLeft = (await button.boundingBox())!.x;
  await button.click();
  const picker = page.getByRole('dialog', { name: 'Articles' });
  // The list opens over the text, just under the button that opened it, as a popover: the text stays where it was.
  await expect(picker).toBeVisible();
  const bar = (await toolbar.boundingBox())!;
  const box = (await picker.boundingBox())!;
  expect(Math.abs(box.y - (bar.y + bar.height))).toBeLessThanOrEqual(8);
  expect(Math.abs(box.x - buttonLeft)).toBeLessThanOrEqual(1);
  expect(box.x + box.width).toBeLessThanOrEqual(bar.x + bar.width);
  expect((await text.boundingBox())!.y).toBe(textTop);
  await expect(picker.getByRole('option', { name: `Billing basics ${suffix}` })).toHaveCount(0);
  await picker.getByRole('option', { name: `Reset your password ${suffix}` }).click();
  await expect(picker).toBeHidden();
  await expect(text).toBeFocused();
  await expect(text).toContainText(`[Reset your password ${suffix}](${address})`);

  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  // The article is published: the save is an unpublished change until Publish changes puts it live.
  await page.getByRole('button', { name: 'Publish changes' }).click();
  await expect(page.getByRole('group', { name: 'Unpublished changes' })).toHaveCount(0);
  const stored = await (await page.request.get(`${baseURL}/api/v1/kb/articles/${editing.id}`)).json() as { body_md: string };
  expect(stored.body_md).toBe(`To change it, [Reset your password ${suffix}](${address})`);

  // On the help center the link opens the other article, under whatever path the help center lives.
  const reader = await page.context().newPage();
  await reader.goto(editing.url);
  await reader.locator('.prose').getByRole('link', { name: `Reset your password ${suffix}` }).click();
  await expect(reader).toHaveURL(target.url);
  await reader.close();

  // With Find open at the top of the text, the list still sits on top: its search box takes the click.
  await text.click();
  await page.keyboard.press('ControlOrMeta+f');
  await expect(page.getByRole('textbox', { name: 'Find' })).toBeVisible();
  await button.click();
  const search = picker.getByRole('combobox');
  const at = (await search.boundingBox())!;
  const hit = await page.evaluate(([x, y]) => !!document.elementFromPoint(x, y)?.closest('[role="dialog"][aria-label="Articles"]'),
    [at.x + 12, at.y + at.height / 2]);
  expect(hit).toBe(true);
});

test('knowledge base: on a phone, Link to article under More formatting is above the article actions bar and links the article', async ({ page }) => {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspace(`Phone Links ${suffix}`, `admin-${suffix}@acme.example`, baseURL));
  const headers = { Origin: baseURL };
  const collection = await page.request.post(`${baseURL}/api/v1/kb/collections`, { headers, data: { name: 'Guides' } });
  expect(collection.status()).toBe(201);
  const collectionId = (await collection.json() as { id: string }).id;
  const make = async (title: string, body_md: string) => {
    const response = await page.request.post(`${baseURL}/api/v1/kb/articles`, { headers, data: { collection_id: collectionId, title, body_md, status: 'published' } });
    expect(response.status()).toBe(201);
    return await response.json() as { id: string; url: string };
  };
  await make(`Reset your password ${suffix}`, 'Open Settings.');
  const editing = await make(`Billing basics ${suffix}`, 'To change it, ');
  const on = await page.request.put(`${baseURL}/api/v1/help-center`, { headers, data: { enabled: true, title: '', locale: 'en' } });
  expect(on.status()).toBe(200);

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${baseURL}/knowledge/articles/${editing.id}`);
  await writeInMarkdown(page);
  await page.getByRole('tab', { name: 'Write' }).click();
  const text = page.getByRole('textbox', { name: 'Text (Markdown)' });
  await text.click();
  await page.keyboard.press('ControlOrMeta+End');

  await page.getByRole('toolbar', { name: 'Formatting' }).getByRole('button', { name: 'More formatting' }).click();
  // The sheet covers the article actions bar, so every tool in it, the last one too, takes a tap.
  const item = page.getByRole('menuitem', { name: 'Link to article' });
  const actions = (await page.getByRole('region', { name: 'Article actions' }).boundingBox())!;
  const itemBox = (await item.boundingBox())!;
  const covering = await page.evaluate(([x, y]) => document.elementFromPoint(x, y)?.closest('[role="region"]')?.getAttribute('aria-label') ?? null,
    [itemBox.x + itemBox.width / 2, itemBox.y + itemBox.height / 2]);
  expect(covering, `item at y=${itemBox.y}, actions bar from y=${actions.y}`).toBeNull();
  await item.click({ timeout: 5000 });
  const picker = page.getByRole('dialog', { name: 'Articles' });
  await picker.getByRole('option', { name: `Reset your password ${suffix}` }).click();
  await expect(picker).toBeHidden();
  await expect(text).toContainText(`[Reset your password ${suffix}](`);
});
