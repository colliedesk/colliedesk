import { EditorSelection, EditorState } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { expect, test } from 'vitest';
import { articleAddress, insertArticleLink } from './article-link';

// jsdom lays nothing out; CodeMirror measures text ranges, so they answer with empty boxes.
if (!('getClientRects' in Range.prototype)) Object.assign(Range.prototype, { getClientRects: () => [], getBoundingClientRect: () => new DOMRect() });

const reset = { title: 'Reset your password', url: 'https://acme.example.com/a/12-reset-your-password' };

function viewOf(doc: string, selection: EditorSelection) {
  return new EditorView({ state: EditorState.create({ doc, selection, extensions: [EditorState.allowMultipleSelections.of(true)] }), parent: document.body });
}

test('the selected text becomes the link text', () => {
  const view = viewOf('See how to reset it.', EditorSelection.single(8, 20));
  insertArticleLink(view, reset);
  expect(view.state.doc.toString()).toBe('See how [to reset it.](/a/12-reset-your-password)');
  expect(view.state.selection.main.empty).toBe(true);
  expect(view.state.selection.main.head).toBe('See how [to reset it.](/a/12-reset-your-password)'.length);
});

test('without a selection the article title is the link text, and the cursor ends after the link', () => {
  const view = viewOf('See  now.', EditorSelection.single(4));
  insertArticleLink(view, reset);
  expect(view.state.doc.toString()).toBe('See [Reset your password](/a/12-reset-your-password) now.');
  expect(view.state.selection.main.head).toBe('See [Reset your password](/a/12-reset-your-password)'.length);
});

test('the address is relative to the help center host, whether it lives on the workspace host or under a path', () => {
  expect(articleAddress('https://acme.example.com/a/12-reset-your-password')).toBe('/a/12-reset-your-password');
  expect(articleAddress('https://app.example.com/help/acme/a/7-billing')).toBe('/a/7-billing');
  expect(articleAddress('https://help.acme.example/a/9')).toBe('/a/9');
  expect(articleAddress('https://help.acme.example/a/5-caf%C3%A9?x=1#top')).toBe('/a/5-caf%C3%A9');
});

test('square brackets in a title or a selection cannot end the link text early', () => {
  const view = viewOf('x', EditorSelection.single(1));
  insertArticleLink(view, { title: 'Export [beta] data', url: 'https://acme.example.com/a/3-export-data' });
  expect(view.state.doc.toString()).toBe('x[Export \\[beta\\] data](/a/3-export-data)');
  const selected = viewOf('see [1] here', EditorSelection.single(4, 7));
  insertArticleLink(selected, reset);
  expect(selected.state.doc.toString()).toBe('see [\\[1\\]](/a/12-reset-your-password) here');
});

test('every selection gets its own link', () => {
  const view = viewOf('a b', EditorSelection.create([EditorSelection.range(0, 1), EditorSelection.cursor(3)]));
  insertArticleLink(view, reset);
  expect(view.state.doc.toString()).toBe('[a](/a/12-reset-your-password) b[Reset your password](/a/12-reset-your-password)');
});

test('a selection is Markdown and keeps its meaning: escapes and code stay as written', () => {
  const escaped = viewOf('\\*a\\*', EditorSelection.single(0, 5));
  insertArticleLink(escaped, reset);
  expect(escaped.state.doc.toString()).toBe('[\\*a\\*](/a/12-reset-your-password)');
  const code = viewOf('run `a]b` now', EditorSelection.single(4, 9));
  insertArticleLink(code, reset);
  expect(code.state.doc.toString()).toBe('run [`a]b`](/a/12-reset-your-password) now');
});

test('a title is plain text: Markdown signs in it show as written', () => {
  const view = viewOf('', EditorSelection.single(0));
  insertArticleLink(view, { title: 'Use *.csv or my_file `x`', url: 'https://acme.example.com/a/4-csv' });
  expect(view.state.doc.toString()).toBe('[Use \\*.csv or my\\_file \\`x\\`](/a/4-csv)');
});

test('right after an exclamation mark the link stays a link, not an image', () => {
  const view = viewOf('Read this!', EditorSelection.single(10));
  insertArticleLink(view, reset);
  expect(view.state.doc.toString()).toBe('Read this\\![Reset your password](/a/12-reset-your-password)');
  expect(view.state.selection.main.head).toBe(view.state.doc.length);
});
