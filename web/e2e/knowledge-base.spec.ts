import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

// The smallest PNG there is: one transparent pixel.
const pixel = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=', 'base64');

test('knowledge base: a collection, an article written in Markdown with a pasted image, published, edited, restored from history, deleted and restored from the trash', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`KB Desk ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);

  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Knowledge' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Knowledge base' })).toBeVisible();
  const newCollection = page.getByRole('form', { name: 'New collection' });
  await newCollection.getByRole('textbox', { name: 'Name' }).fill('Getting started');
  await newCollection.getByRole('button', { name: 'Create collection' }).click();
  await expect(page.getByRole('link', { name: 'Getting started, 0 articles' })).toHaveAttribute('aria-current', 'page');

  await page.getByRole('link', { name: 'New article' }).click();
  await page.getByRole('textbox', { name: 'Title' }).fill('Reset your password');
  const text = page.getByRole('textbox', { name: 'Text' });
  await text.fill('## Steps\n\n| Step | Where |\n| --- | --- |\n| 1 | Settings |\n\n');
  // Pasting an image uploads it and writes its address where the cursor is.
  await text.evaluate((field: HTMLTextAreaElement, bytes) => {
    field.focus();
    field.setSelectionRange(field.value.length, field.value.length);
    const data = new DataTransfer();
    data.items.add(new File([new Uint8Array(bytes)], 'login.png', { type: 'image/png' }));
    field.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
  }, [...pixel]);
  await expect(text).toHaveValue(/!\[login\]\(\/api\/v1\/kb\/images\/[0-9a-f-]{36}\)/);
  await page.getByRole('tab', { name: 'Preview' }).click();
  const preview = page.getByRole('tabpanel', { name: 'Preview' });
  await expect(preview.getByRole('heading', { name: 'Steps' })).toBeVisible();
  await expect(preview.getByRole('table')).toContainText('Settings');
  await expect(preview.getByRole('img', { name: 'login' })).toHaveJSProperty('naturalWidth', 1);
  await page.getByRole('button', { name: 'Save and publish' }).click();
  await expect(page).toHaveURL(/\/knowledge\/articles\/[0-9a-f-]{36}$/);
  await expect(page.getByRole('button', { name: 'Unpublish' })).toBeVisible();

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
