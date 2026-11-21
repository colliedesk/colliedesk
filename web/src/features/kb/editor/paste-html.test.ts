import { expect, test } from 'vitest';
import shared from '../../../../../server/internal/freshdesk/testdata/html-markdown-cases.json';
import { googleDocsHTML } from './paste-html.test.fixture';
import { pastedMarkdown } from './paste-html';

// The same cases as the Freshdesk import (TestArticleMarkdownMatchesTheSharedPasteCases): HTML pasted into
// the editor and the same HTML imported from Freshdesk become the same Markdown.
test.each(shared.cases)('pasted HTML becomes the Markdown the import writes: $name', ({ html, markdown }) => {
  expect(pastedMarkdown(html).markdown).toBe(markdown);
});

test('a Google Docs copy keeps its heading, list, link, table, emphasis and image, and leaves its styling', () => {
  const pasted = pastedMarkdown(googleDocsHTML);
  expect(pasted.markdown).toBe([
    '## Getting started',
    '- Open the app\n- Choose **Settings**',
    'See [the help page](https://example.com/help).',
    // Google Docs marks no header row, so the table gets an empty one, as the Freshdesk import does.
    '|      |       |\n|------|-------|\n| Plan | Seats |\n| Team | 5     |',
    '![](https://lh7-rt.googleusercontent.com/docsz/AD_4nXcopy/photo.png)',
    '**Bold** and *italic* and plain',
  ].join('\n\n'));
  expect(pasted.images).toEqual(['https://lh7-rt.googleusercontent.com/docsz/AD_4nXcopy/photo.png']);
});

test('a backslash and a pipe in a table cell stay text in that cell, also inside code', () => {
  // GFM reads \\ as a backslash and \| as a pipe, both in text and in a code span in a table.
  const pasted = pastedMarkdown('<table><tr><th>Path</th></tr><tr><td>C:\\dir | x</td></tr><tr><td><code>a\\|b</code></td></tr></table>');
  expect(pasted.markdown).toBe([
    '| Path         |',
    '|--------------|',
    '| C:\\\\dir \\| x |',
    '| `a\\\\|b`      |',
  ].join('\n'));
});

test('images to store are listed once each, and a javascript: or file: image is left out', () => {
  const pasted = pastedMarkdown('<p><img src="data:image/png;base64,iVBORw0KGgo=" alt="Pasted"><img src="javascript:alert(1)" alt="bad">'
    + '<img src="file:///etc/passwd" alt="file"><img src="https://example.com/a.png" alt="A"><img src="https://example.com/a.png" alt="A again">'
    + '<img src="/api/v1/kb/images/7f0c" alt="Ours"></p>');
  expect(pasted.markdown).toBe('![Pasted](data:image/png;base64,iVBORw0KGgo=)![A](https://example.com/a.png)![A again](https://example.com/a.png)'
    + '![Ours](/api/v1/kb/images/7f0c)');
  expect(pasted.images).toEqual(['data:image/png;base64,iVBORw0KGgo=', 'https://example.com/a.png']);
});

test('an event handler, an iframe or an svg never reaches the Markdown', () => {
  const pasted = pastedMarkdown('<p onclick="alert(1)">Safe <svg onload="alert(1)"><text>x</text></svg>'
    + '<iframe src="https://evil.example"></iframe><object data="x"></object></p>');
  expect(pasted.markdown).toBe('Safe');
});
