import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { googleDocsHTML } from '../src/features/kb/editor/paste-html.test.fixture';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

// The smallest PNG there is: one transparent pixel.
const pixel = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=';
const headers = { Origin: servers.main };

async function signIn(page: Page, name: string) {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`${name} ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const { id } = await (await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Billing' } })).json() as { id: string };
  return id;
}

test('knowledge base: text typed into a published article survives closing the tab, and stays off the help center and out of the history', async ({ page, context }) => {
  const collection = await signIn(page, 'KB Autosave');
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Autosave help', locale: 'en' } })).status()).toBe(200);
  const article = await (await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers,
    data: { collection_id: collection, title: 'Get a VAT invoice', body_md: 'Ask within 30 days.', status: 'published' } })).json() as { id: string; url: string };
  const versions = async () => ((await (await page.request.get(`${servers.main}/api/v1/kb/articles/${article.id}/versions`)).json()) as { items: unknown[] }).items.length;
  const before = await versions();

  const tab = await context.newPage();
  await tab.goto(`${servers.main}/knowledge/articles/${article.id}`);
  await tab.getByRole('textbox', { name: 'Text (Markdown)' }).click();
  await tab.keyboard.press('ControlOrMeta+End');
  await tab.keyboard.type(' Or call the billing desk.');
  // No Save: the person stops typing and closes the tab 5 s later.
  await tab.waitForTimeout(5_000);
  await tab.close({ runBeforeUnload: true });

  await page.goto(`${servers.main}/knowledge/articles/${article.id}`);
  await expect(page.getByRole('textbox', { name: 'Text (Markdown)' })).toHaveText('Ask within 30 days. Or call the billing desk.');
  await expect(page.getByRole('group', { name: 'Unpublished changes' })).toBeVisible();
  // The server copy is the newest, so nothing is offered back from the device.
  await expect(page.getByRole('group', { name: /^Restore unsaved text from/ })).toHaveCount(0);
  expect(await (await page.request.get(article.url)).text()).not.toContain('Or call the billing desk.');
  expect(await versions()).toBe(before);
});

test('knowledge base: a Google Docs copy pastes as Markdown with its image stored in the media library, and one click makes it plain text', async ({ page }) => {
  const collection = await signIn(page, 'KB Paste');
  const article = await (await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers,
    data: { collection_id: collection, title: 'Getting started', body_md: 'Draft', status: 'draft' } })).json() as { id: string };
  await page.goto(`${servers.main}/knowledge/articles/${article.id}`);
  const text = page.getByRole('textbox', { name: 'Text (Markdown)' });
  await text.click();
  await page.keyboard.press('ControlOrMeta+A');
  const copy = googleDocsHTML.replace('https://lh7-rt.googleusercontent.com/docsz/AD_4nXcopy/photo.png', pixel);
  await text.evaluate((field, html) => {
    const data = new DataTransfer();
    data.setData('text/html', html);
    data.setData('text/plain', 'Getting started\nOpen the app\nChoose Settings');
    field.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
  }, copy);
  await expect(text).toContainText(/!\[\]\(\/api\/v1\/kb\/images\/[0-9a-f-]{36}\)/);

  const toast = page.getByRole('group', { name: 'Pasted text' });
  await toast.getByRole('button', { name: 'Paste as plain text' }).click();
  await expect(text).toHaveText('Getting startedOpen the appChoose Settings');
  await toast.getByRole('button', { name: 'Keep formatting' }).click();
  await expect(text).toContainText('## Getting started');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByText('Unsaved changes')).toHaveCount(0);

  // The image is in the media library, and the text names it by its library address.
  const library = await (await page.request.get(`${servers.main}/api/v1/kb/media?kind=image`)).json() as { items: { url: string }[] };
  expect(library.items).toHaveLength(1);
  const saved = await (await page.request.get(`${servers.main}/api/v1/kb/articles/${article.id}`)).json() as { body_md: string };
  expect(saved.body_md.trimEnd()).toBe([
    '## Getting started',
    '- Open the app\n- Choose **Settings**',
    'See [the help page](https://example.com/help).',
    '|      |       |\n|------|-------|\n| Plan | Seats |\n| Team | 5     |',
    `![](${library.items[0].url})`,
    '**Bold** and *italic* and plain',
  ].join('\n\n'));
});
