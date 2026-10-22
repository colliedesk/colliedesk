import path from 'node:path';
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
  const text = page.getByRole('textbox', { name: 'Text (Markdown)' });
  await text.click();
  await page.keyboard.type('## Steps');
  await page.keyboard.press('Enter');
  await page.keyboard.press('Enter');
  await page.getByRole('toolbar', { name: 'Formatting' }).getByRole('button', { name: 'More formatting' }).click();
  await page.getByRole('menuitem', { name: 'Checklist' }).click();
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
  await page.getByRole('button', { name: 'More article actions' }).click();
  await expect(page.getByRole('menuitem', { name: 'Unpublish' })).toBeVisible();
  await page.keyboard.press('Escape');
  const id = page.url().split('/').pop()!;
  const saved = await (await page.request.get(`${servers.main}/api/v1/kb/articles/${id}`)).json() as { body_md: string };
  expect(saved.body_md).toMatch(/^## Steps\n\n- \[ \] Open Settings\n\n!\[login\]\(\/api\/v1\/kb\/images\/[0-9a-f-]{36}\)\n?$/);

  await page.getByRole('textbox', { name: 'Title' }).fill('Reset a password');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'More article actions' }).click();
  await page.getByRole('menuitem', { name: 'Version history' }).click();
  const history = page.getByRole('region', { name: 'History of Reset a password' });
  await history.getByRole('button', { name: 'Restore version 1' }).click();
  await expect(page.getByRole('textbox', { name: 'Title' })).toHaveValue('Reset your password');

  await page.getByRole('button', { name: 'Close history' }).click();
  await page.getByRole('button', { name: 'More article actions' }).click();
  await page.getByRole('menuitem', { name: 'Delete article…' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Move to trash' }).click();
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
  await page.getByRole('button', { name: 'More article actions' }).click();
  await expect(page.getByRole('menuitem', { name: 'Unpublish' })).toBeVisible();
  await page.keyboard.press('Escape');
  const stored = await (await page.request.get(`${servers.main}/api/v1/kb/articles/${id}`)).json() as { body_md: string; status: string };
  expect(stored).toMatchObject({ body_md: body, status: 'published' });
  expect(external).toEqual([]);

  // Find and replace (Ctrl+F, Cmd+F) is CodeMirror's panel inside the editor.
  await page.getByRole('tab', { name: 'Write' }).click();
  await page.getByRole('textbox', { name: 'Text (Markdown)' }).click();
  await page.keyboard.press('ControlOrMeta+f');
  await expect(page.getByRole('textbox', { name: 'Find' })).toBeFocused();
});

test('knowledge base: Insert video puts a YouTube player in the article, which the preview and the help center show sandboxed from youtube-nocookie.com', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url, slug } = createWorkspaceWithSlug(`KB Video ${suffix}`, `admin-${suffix}@example.com`);
  // The player's site is outside the test: every request to it is answered empty and counted.
  const players: string[] = [];
  await page.route(/youtube(-nocookie)?\.com|vimeo\.com/, (route) => { players.push(route.request().url()); return route.fulfill({ status: 204 }); });
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Video help', locale: 'en' } })).status()).toBe(200);
  const created = await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Guides' } });
  const { id: collection } = await created.json() as { id: string };
  const article = await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers, data: { collection_id: collection, title: 'Tour', body_md: 'Watch this.' } });
  const { id } = await article.json() as { id: string };

  await page.goto(`${servers.main}/knowledge/articles/${id}`);
  await page.getByRole('tab', { name: 'Write' }).click();
  await page.getByRole('textbox', { name: 'Text (Markdown)' }).click();
  await page.keyboard.press('ControlOrMeta+End');
  await page.getByRole('button', { name: 'Insert image or video' }).click();
  await page.getByRole('menuitem', { name: /^YouTube or Vimeo link/ }).click();
  const link = page.getByRole('textbox', { name: 'YouTube or Vimeo link' });
  await expect(link).toBeFocused();
  await link.fill('https://evil.example/watch?v=x');
  await link.press('Enter');
  await expect(page.getByRole('alert')).toContainText('evil.example is not a video site');
  await link.fill('https://youtu.be/jNQXAC9IVRw?t=65');
  await link.press('Enter');
  await expect(link).toBeHidden();
  await page.getByRole('tab', { name: 'Preview' }).click();
  const frame = page.getByRole('region', { name: 'Preview' }).locator('iframe');
  await expect(frame).toHaveAttribute('src', 'https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=65');
  await expect(frame).toHaveAttribute('sandbox', 'allow-scripts allow-same-origin allow-presentation allow-popups');
  await page.getByRole('button', { name: 'Save and publish' }).click();
  await page.getByRole('button', { name: 'More article actions' }).click();
  await expect(page.getByRole('menuitem', { name: 'Unpublish' })).toBeVisible();
  await page.keyboard.press('Escape');

  const response = await page.goto(`${servers.main}/help/${slug}/a/1-tour`);
  expect(response?.headers()['content-security-policy']).toContain('frame-src https://www.youtube-nocookie.com https://player.vimeo.com');
  const player = page.locator('iframe');
  await expect(player).toHaveAttribute('src', 'https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=65');
  await expect(player).toHaveAttribute('referrerpolicy', 'strict-origin-when-cross-origin');
  await expect(player).toHaveAttribute('sandbox', 'allow-scripts allow-same-origin allow-presentation allow-popups');
  expect(players.every((address) => address.startsWith('https://www.youtube-nocookie.com/embed/'))).toBe(true);
});

test('knowledge base: a WebM uploaded in the media library plays in a published article on the help center, with seeking', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url, slug } = createWorkspaceWithSlug(`KB Media ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);

  await page.goto(`${servers.main}/knowledge/media`);
  await expect(page.getByRole('heading', { level: 1, name: 'Media' })).toBeVisible();
  await page.locator('input[type="file"]').setInputFiles(path.resolve(import.meta.dirname, '../../server/internal/media/testdata/video/vp9.webm'));
  await expect(page.getByRole('region', { name: /upload/ })).toContainText('1 added');
  const card = page.getByRole('button', { name: /^vp9, video,/ });
  await card.click();
  const panel = page.getByRole('complementary', { name: 'vp9' });
  await panel.getByRole('textbox', { name: 'Name' }).fill('Product tour');
  await panel.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('button', { name: /^Product tour, video,/ })).toBeVisible();
  const id = new URL(page.url()).searchParams.get('item')!;

  const headers = { Origin: servers.main };
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Video help', locale: 'en' } })).status()).toBe(200);
  const { id: collection } = await (await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Guides' } })).json() as { id: string };
  const { id: article } = await (await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers, data: { collection_id: collection, title: 'Tour', body_md: 'Watch this.' } })).json() as { id: string };

  // The editor's Media button puts the library video in the article; the preview shows it.
  await page.goto(`${servers.main}/knowledge/articles/${article}`);
  await page.getByRole('tab', { name: 'Write' }).click();
  await page.getByRole('textbox', { name: 'Text (Markdown)' }).click();
  await page.keyboard.press('ControlOrMeta+End');
  await page.getByRole('button', { name: 'Insert image or video' }).click();
  await page.getByRole('menuitem', { name: /^From the media library/ }).click();
  const picker = page.getByRole('dialog', { name: 'Insert from the media library' });
  await picker.getByRole('option', { name: /^Product tour, video,/ }).click();
  await picker.getByRole('button', { name: 'Insert 1' }).click();
  await expect(page.getByRole('textbox', { name: 'Text (Markdown)' })).toContainText(`<video controls preload="metadata" src="/api/v1/kb/media/${id}" title="Product tour"></video>`);
  await page.getByRole('tab', { name: 'Preview' }).click();
  await expect(page.getByRole('region', { name: 'Preview' }).locator('video')).toHaveAttribute('src', `/api/v1/kb/media/${id}`);
  await page.getByRole('button', { name: 'Save and publish' }).click();
  await page.getByRole('button', { name: 'More article actions' }).click();
  await expect(page.getByRole('menuitem', { name: 'Unpublish' })).toBeVisible();
  await page.keyboard.press('Escape');

  await page.goto(`${servers.main}/help/${slug}/a/1-tour`);
  const player = page.locator('video');
  await expect(player).toHaveAttribute('src', `/help/${slug}/media/${id}`);
  // Chromium of Playwright plays WebM (it has no H.264): the video reaches its end after a seek.
  await player.evaluate(async (video: HTMLVideoElement) => {
    video.muted = true;
    await new Promise((resolve) => { if (video.readyState >= 1) resolve(null); else video.addEventListener('loadedmetadata', resolve, { once: true }); });
    video.currentTime = video.duration / 2;
    await new Promise((resolve) => video.addEventListener('seeked', resolve, { once: true }));
  });
  const range = await page.request.get(`${servers.main}/help/${slug}/media/${id}`, { headers: { Range: 'bytes=0-99' } });
  expect(range.status()).toBe(206);
  expect(range.headers()['content-type']).toBe('video/webm');
});

test('knowledge base: an article excluded from AI says since when and by whom, keeps it after a reload, shows it in the history, and lets AI read it again', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`KB AI ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Knowledge' }).click();
  const newCollection = page.getByRole('form', { name: 'New collection' });
  await newCollection.getByRole('textbox', { name: 'Name' }).fill('Billing');
  await newCollection.getByRole('button', { name: 'Create collection' }).click();
  await page.getByRole('link', { name: 'New article' }).click();
  await page.getByRole('textbox', { name: 'Title' }).fill('Internal refund rules');
  await page.getByRole('textbox', { name: 'Text (Markdown)' }).click();
  await page.keyboard.type('Refunds above 100 need a second approval.');
  await page.getByRole('button', { name: 'Save and publish' }).click();
  await expect(page).toHaveURL(/\/knowledge\/articles\/[0-9a-f-]{36}$/);

  const box = page.getByRole('region', { name: 'Used by AI' });
  await expect(box).toContainText('When AI is on, reply drafts and suggestions can quote this article.');
  await box.getByRole('button', { name: 'Exclude from AI' }).click();
  await expect(box).toContainText(/Off for this article since .+, by /);
  await expect(page.getByText('AI does not read this article.')).toBeVisible();
  await page.reload();
  await expect(page.getByRole('region', { name: 'Used by AI' }).getByRole('button', { name: 'Let AI use it again' })).toBeVisible();
  await page.getByRole('button', { name: 'More article actions' }).click();
  await page.getByRole('menuitem', { name: 'Version history' }).click();
  await expect(page.getByRole('dialog', { name: 'Version history' })).toContainText('Excluded from AI');
  await page.getByRole('button', { name: 'Close history' }).click();
  await page.locator('.kb-ai-note').getByRole('button', { name: 'Let AI use it again' }).click();
  await expect(page.getByRole('region', { name: 'Used by AI' }).getByRole('button', { name: 'Exclude from AI' })).toBeVisible();
  await expect(page.getByText('AI does not read this article.')).toHaveCount(0);
});
