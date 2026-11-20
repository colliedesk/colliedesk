import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

// The How-to starter every new workspace has, word for word.
const howTo = 'Say in one sentence what the reader can do after these steps.\n\n## Before you start\n\n- What the reader needs (a role, a plan, a setting).\n\n'
  + '## Steps\n\n1. First step.\n2. Second step.\n3. Third step.\n\n## Check that it worked\n\nWhat the reader sees when the task is done.\n\n'
  + '## Related articles\n\n- Link to the next task.\n';

test('knowledge base templates: a new workspace has the starters, an article starts from one, another is inserted, and a collection default leads the menu', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`KB Templates ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);

  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Knowledge base' }).click();
  const newCollection = page.getByRole('form', { name: 'New collection' });
  await newCollection.getByRole('textbox', { name: 'Name' }).fill('Getting started');
  await newCollection.getByRole('button', { name: 'Create collection' }).click();
  await expect(page.getByRole('link', { name: 'Getting started, 0 articles' })).toHaveAttribute('aria-current', 'page');

  await page.getByRole('link', { name: 'Templates', exact: true }).click();
  const list = page.getByRole('list', { name: 'Article templates' });
  await expect(list.getByRole('listitem')).toHaveCount(3);
  for (const name of ['How-to', 'Troubleshooting', 'FAQ']) await expect(list.getByText(name, { exact: true })).toBeVisible();

  await page.getByRole('link', { name: /^Articles/ }).click();
  await page.getByRole('link', { name: 'Getting started, 0 articles' }).click();
  await page.getByRole('button', { name: 'New article' }).click();
  await page.getByRole('menuitem', { name: /^How-to/ }).click();
  const text = page.getByRole('textbox', { name: 'Text (Markdown)' });
  await expect(text).toContainText('## Before you start');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Install the app');
  await page.getByRole('button', { name: 'Save draft' }).click();
  await expect(page).toHaveURL(/\/knowledge\/articles\/[0-9a-f-]{36}$/);
  const id = page.url().split('/').pop()!;
  const saved = await (await page.request.get(`${servers.main}/api/v1/kb/articles/${id}`)).json() as { body_md: string };
  expect(saved.body_md.trimEnd()).toBe(howTo.trimEnd());

  // Insert template adds another template's text where the cursor is; saving keeps both.
  await text.click();
  await page.keyboard.press('Control+End');
  await page.getByRole('button', { name: 'Insert template' }).click();
  await page.getByRole('menu', { name: 'Insert template' }).getByRole('menuitem', { name: /^FAQ/ }).click();
  await expect(text).toContainText('## Question one?');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  const edited = await (await page.request.get(`${servers.main}/api/v1/kb/articles/${id}`)).json() as { body_md: string };
  expect(edited.body_md).toContain('## Related articles');
  expect(edited.body_md).toContain('## Question one?');

  // A collection's default template is listed first, under Blank.
  await page.goto(`${servers.main}/knowledge`);
  await page.getByRole('link', { name: 'Getting started, 1 article' }).click();
  await page.getByRole('button', { name: 'Edit Getting started' }).click();
  const form = page.getByRole('form', { name: 'Edit Getting started' });
  await form.getByRole('combobox', { name: 'Default template' }).selectOption({ label: 'FAQ' });
  await form.getByRole('button', { name: 'Save collection' }).click();
  await expect(form).toBeHidden();
  await page.getByRole('button', { name: 'New article' }).click();
  const items = page.getByRole('menu', { name: 'New article' }).getByRole('menuitem');
  await expect(items.nth(0)).toHaveText(/^Blank/);
  await expect(items.nth(1)).toHaveText('FAQDefault for this collection');
});
