import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

// The smallest PNG there is: one transparent pixel.
const pixel = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=', 'base64');

test('knowledge base: a collection, an article written in the Markdown editor with a pasted image, published, edited, restored from history, deleted and restored from the trash', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`KB Desk ${suffix}`, `admin-${suffix}@example.com`);
  // The article list never loads the editor's chunk; an article page does.
  const editorChunks: string[] = [];
  page.on('request', (request) => { if (/codemirror-editor/.test(request.url())) editorChunks.push(request.url()); });
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);

  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Knowledge' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Knowledge base' })).toBeVisible();
  const newCollection = page.getByRole('form', { name: 'New collection' });
  await newCollection.getByRole('textbox', { name: 'Name' }).fill('Getting started');
  await newCollection.getByRole('button', { name: 'Create collection' }).click();
  await expect(page.getByRole('link', { name: 'Getting started, 0 articles' })).toHaveAttribute('aria-current', 'page');
  expect(editorChunks).toEqual([]);

  await page.getByRole('link', { name: 'New article' }).click();
  await page.getByRole('textbox', { name: 'Title' }).fill('Reset your password');
  const text = page.getByRole('textbox', { name: 'Text' });
  await text.click();
  await page.keyboard.type('## Steps');
  await page.keyboard.press('Enter');
  await page.keyboard.press('Enter');
  await page.getByRole('toolbar', { name: 'Formatting' }).getByRole('button', { name: 'Checklist' }).click();
  await page.keyboard.type('Open Settings');
  // A pasted image is uploaded and becomes a paragraph of its own after the line; nothing holds its place in the text.
  await text.evaluate((field, bytes) => {
    const data = new DataTransfer();
    data.items.add(new File([new Uint8Array(bytes)], 'login.png', { type: 'image/png' }));
    field.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
  }, [...pixel]);
  await expect(text).toContainText(/!\[login\]\(\/api\/v1\/kb\/images\/[0-9a-f-]{36}\)/);
  await page.getByRole('tab', { name: 'Preview' }).click();
  const preview = page.getByRole('region', { name: 'Preview' });
  await expect(preview.getByRole('heading', { name: 'Steps' })).toBeVisible();
  await expect(preview.getByRole('checkbox')).not.toBeChecked();
  await expect(preview.getByRole('img', { name: 'login' })).toHaveJSProperty('naturalWidth', 1);
  await page.getByRole('button', { name: 'Save and publish' }).click();
  await expect(page).toHaveURL(/\/knowledge\/articles\/[0-9a-f-]{36}$/);
  await expect(page.getByRole('button', { name: 'Unpublish' })).toBeVisible();
  const id = page.url().split('/').pop()!;
  const saved = await (await page.request.get(`${servers.main}/api/v1/kb/articles/${id}`)).json() as { body_md: string };
  expect(saved.body_md).toMatch(/^## Steps\n\n- \[ \] Open Settings\n\n!\[login\]\(\/api\/v1\/kb\/images\/[0-9a-f-]{36}\)\n?$/);

  await page.getByRole('textbox', { name: 'Title' }).fill('Reset a password');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'History' }).click();
  const history = page.getByRole('region', { name: 'History of Reset a password' });
  await history.getByRole('button', { name: 'Restore version 1' }).click();
  await expect(page.getByRole('textbox', { name: 'Title' })).toHaveValue('Reset your password');

  await page.getByRole('button', { name: 'Delete' }).click();
  await page.getByRole('button', { name: 'Move to trash' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Knowledge base' })).toBeVisible();
  await expect(page.getByText('No articles here yet.')).toBeVisible();
  await page.getByRole('button', { name: 'Trash' }).click();
  await page.getByRole('region', { name: 'Deleted articles' }).getByRole('button', { name: 'Restore Reset your password' }).click();
  await page.getByRole('button', { name: 'Close trash' }).click();
  await expect(page.getByRole('link', { name: /Reset your password/ })).toContainText('Published');
});

test('knowledge base: an article opened and published untouched keeps its Markdown to the byte; raw HTML and images from other sites never load', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`KB Bytes ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  const created = await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Guides' } });
  expect(created.status()).toBe(201);
  const { id: collection } = await created.json() as { id: string };
  // Written by an AI agent or an import: a setext heading, a reference link, a table without padding, raw HTML.
  const body = 'Steps\n-----\n\nSee [the guide][g].\n\n|a|b|\n|-|-|\n|1|2|\n\n<img src="https://tracker.example/x.gif" onerror="window.ran=1">\n\n![tracker](https://tracker.example/p.gif)\n\n[g]: https://example.com/guide\n';
  const article = await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers, data: { collection_id: collection, title: 'Written elsewhere', body_md: body } });
  expect(article.status()).toBe(201);
  const { id } = await article.json() as { id: string };
  const external: string[] = [];
  page.on('request', (request) => { if (request.url().includes('tracker.example')) external.push(request.url()); });

  await page.goto(`${servers.main}/knowledge/articles/${id}`);
  await page.getByRole('tab', { name: 'Preview' }).click();
  const preview = page.getByRole('region', { name: 'Preview' });
  await expect(preview.getByRole('heading', { level: 2, name: 'Steps' })).toBeVisible();
  await expect(preview).toContainText('Image from another site, not shown here or on the help center: tracker');
  await expect(preview.locator('img')).toHaveCount(0);
  expect(await page.evaluate(() => (window as { ran?: number }).ran)).toBeUndefined();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Publish', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Unpublish' })).toBeVisible();
  const stored = await (await page.request.get(`${servers.main}/api/v1/kb/articles/${id}`)).json() as { body_md: string; status: string };
  expect(stored).toMatchObject({ body_md: body, status: 'published' });
  expect(external).toEqual([]);

  // Find and replace (Ctrl+F, Cmd+F) is CodeMirror's panel inside the editor.
  await page.getByRole('tab', { name: 'Write' }).click();
  await page.getByRole('textbox', { name: 'Text' }).click();
  await page.keyboard.press('ControlOrMeta+f');
  await expect(page.getByRole('textbox', { name: 'Find' })).toBeFocused();
});
