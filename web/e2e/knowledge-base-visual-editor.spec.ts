import { expect, test } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

// docs/user/knowledge-base.md "Visual or Markdown": an article opens in the visual editor, what the toolbar
// builds is saved as the Markdown (and the HTML table, for merged cells) the server keeps, and switching to the
// Markdown source and back changes nothing.
const stored = '## Steps\n\n**Open Settings**\n\n'
  + '<table>\n<tr>\n<th colspan="2">\n\nPlan\n\nSeats\n\n</th>\n</tr>\n<tr>\n<td>\n\nTeam\n\n</td>\n<td>\n\n5\n\n</td>\n</tr>\n</table>\n';

test('knowledge base: a new article opens in the visual editor, a heading, bold text and a merged-cell table save and read back the same, and Markdown and back changes nothing', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url } = createWorkspaceWithSlug(`KB Visual ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  const { id: collection } = await (await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Guides' } })).json() as { id: string };
  // Wide enough for every tool on the toolbar, Table among them, outside More formatting.
  await page.setViewportSize({ width: 1800, height: 900 });
  await page.goto(`${servers.main}/knowledge?collection=${collection}`);
  await page.getByRole('button', { name: 'New article' }).click();
  await page.getByRole('menuitem', { name: /^Blank/ }).click();

  // Visual is the editor until the person chooses another.
  const kind = page.getByRole('group', { name: 'Editor type' });
  await expect(kind.getByRole('button', { name: 'Visual' })).toHaveAttribute('aria-pressed', 'true');
  await expect(kind.getByRole('button', { name: 'Markdown' })).toHaveAttribute('aria-pressed', 'false');
  const text = page.getByRole('textbox', { name: 'Text', exact: true });
  await expect(page.getByRole('textbox', { name: 'Text (Markdown)' })).toHaveCount(0);

  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Add a seat');
  await text.click();
  await page.getByRole('combobox', { name: 'Block type' }).selectOption({ label: 'Heading 2' });
  await text.click();
  await page.keyboard.type('Steps');
  await page.keyboard.press('Enter');
  const toolbar = page.getByRole('toolbar', { name: 'Formatting' });
  await toolbar.getByRole('button', { name: /^Bold/ }).click();
  await page.keyboard.type('Open Settings');
  await toolbar.getByRole('button', { name: /^Bold/ }).click();
  await page.keyboard.press('Enter');
  await toolbar.getByRole('button', { name: 'Table', exact: true }).click();
  for (const cell of ['Plan', 'Seats', 'Team']) {
    await page.keyboard.type(cell);
    await page.keyboard.press('Tab');
  }
  await page.keyboard.type('5');
  // The two header cells, selected with the keyboard, become one.
  await text.locator('th', { hasText: 'Plan' }).click();
  await page.keyboard.press('End');
  await page.keyboard.press('Shift+ArrowRight');
  await toolbar.getByRole('button', { name: 'Table tools' }).click();
  await page.getByRole('menu', { name: 'Table tools' }).getByRole('menuitem', { name: 'Merge cells' }).click();
  await expect(text.locator('th[colspan="2"]')).toHaveCount(1);

  await page.getByRole('button', { name: 'Save draft' }).click();
  await expect(page).toHaveURL(/\/knowledge\/articles\/[0-9a-f-]{36}$/);
  const id = page.url().split('/').pop()!;
  const article = async () => (await (await page.request.get(`${servers.main}/api/v1/kb/articles/${id}`)).json() as { body_md: string }).body_md;
  expect(await article()).toBe(stored);

  // After a reload the same article shows the same content, in the same editor.
  await page.reload();
  await expect(kind.getByRole('button', { name: 'Visual' })).toHaveAttribute('aria-pressed', 'true');
  await expect(text.getByRole('heading', { level: 2, name: 'Steps' })).toBeVisible();
  await expect(text.locator('strong')).toHaveText('Open Settings');
  await expect(text.locator('th[colspan="2"]')).toHaveText('PlanSeats');
  await expect(text.locator('td')).toHaveText(['Team', '5']);
  const save = page.getByRole('button', { name: 'Save', exact: true });
  await expect(save).toBeDisabled();

  // The Markdown source shows the same text; switching back changes nothing and leaves nothing to save.
  await kind.getByRole('button', { name: 'Markdown' }).click();
  await expect(page.getByRole('textbox', { name: 'Text (Markdown)' })).toContainText('<th colspan="2">');
  await expect(save).toBeDisabled();
  await kind.getByRole('button', { name: 'Visual' }).click();
  await expect(text.locator('th[colspan="2"]')).toHaveText('PlanSeats');
  await expect(save).toBeDisabled();
  expect(await article()).toBe(stored);
  // The last choice is kept for the person.
  await expect.poll(async () => ((await (await page.request.get(`${servers.main}/api/v1/auth/me`)).json()) as { kb_editor?: string }).kb_editor).toBe('visual');
});
