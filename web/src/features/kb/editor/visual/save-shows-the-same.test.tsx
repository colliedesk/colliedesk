import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import articles from '../../../../../../server/internal/kbhtml/testdata/articles.json';
import normalize from '../../../../../../server/internal/kbformat/testdata/normalize.json';
import rendered from '../../../../../../server/internal/kbformat/testdata/render.json';
import { MarkdownPreview } from '../../markdown-preview';
import { shape } from '../../preview-shape.test.fixture';
import { markdownToDoc } from './from-markdown';
import { docToMarkdown } from './to-markdown';

const save = (text: string) => docToMarkdown(markdownToDoc(text));

function shown(text: string) {
  const { container, unmount } = render(<MarkdownPreview text={text} />);
  container.querySelector('.kb-html-warning')?.remove();
  const out = shape(container.querySelector('.kb-preview')!);
  unmount();
  return out;
}

const corpus = [
  ...normalize.map((c) => [c.name, c.out] as const),
  ...rendered.cases.map((c) => [c.name, c.markdown] as const),
  ...articles.cases.map((c) => [c.name, c.markdown] as const),
  ['other spellings', 'Setext\n===\n\n__bold__ _it_ a\nsoft break  \nhard\n\n* star\n\n+ plus\n\n    indented code\n\n***\n\n| l | c |\n|---|:-:|\n| 1 |\n'],
  ['a loose list', '- one\n\n- two\n'],
  ['escapes', 'a \\* not emphasis, 1\\. not a list, <not a tag, &amp; [brackets]\n'],
] as const;

// Opening an article in the visual editor and saving it shows the same page.
describe('an article saved by the visual editor shows as it did', () => {
  it.each(corpus)('%s', (_name, text) => {
    expect(shown(save(text))).toBe(shown(text));
  });

  // The only changes a reader could tell apart in the page's HTML, and not on the page.
  it('it writes bold italic as bold over italic, and a left-aligned column as one with no alignment', () => {
    expect(save('***both***\n')).toBe('**_both_**\n');
    expect(save('| l |\n|:--|\n| 1 |\n')).toBe('| l |\n| - |\n| 1 |\n');
  });
});
