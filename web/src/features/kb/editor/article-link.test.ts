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
