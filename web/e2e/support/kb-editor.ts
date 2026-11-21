import { expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

// writeInMarkdown switches an open article to the Markdown editor, as a person does with Editor type, and
// returns its text. Articles open in the visual editor until the person chooses; the choice is kept for them,
// so a later article opens in Markdown and this changes nothing.
export async function writeInMarkdown(page: Page): Promise<Locator> {
  const markdown = page.getByRole('group', { name: 'Editor type' }).getByRole('button', { name: 'Markdown' });
  if (await markdown.getAttribute('aria-pressed') !== 'true') await markdown.click();
  await expect(markdown).toHaveAttribute('aria-pressed', 'true');
  return page.getByRole('textbox', { name: 'Text (Markdown)' });
}
