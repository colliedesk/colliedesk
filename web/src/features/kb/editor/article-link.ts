import { EditorSelection } from '@codemirror/state';
import type { EditorView } from '@codemirror/view';

// articleAddress is the part of an article's public address that stays the same on the workspace host and on a
// custom domain: /a/<number>-<slug>. A link written this way keeps working when the help center moves.
export function articleAddress(url: string): string {
  const { pathname } = new URL(url);
  const at = pathname.lastIndexOf('/a/');
  return at < 0 ? pathname : pathname.slice(at);
}

// A bracket or backslash in the link text would end it early or escape what follows.
const escapeText = (text: string) => text.replace(/[\\[\]]/g, (char) => `\\${char}`);

// insertArticleLink writes [text](/a/<n>-slug) for each selection: the selected text is the link text, or the
// article's title when nothing is selected. The cursor ends after the link.
export function insertArticleLink(view: EditorView, article: { title: string; url: string }) {
  const { state } = view;
  const address = articleAddress(article.url);
  view.dispatch(
    state.changeByRange((range) => {
      const text = state.sliceDoc(range.from, range.to) || article.title;
      const insert = `[${escapeText(text)}](${address})`;
      return { changes: { from: range.from, to: range.to, insert }, range: EditorSelection.cursor(range.from + insert.length) };
    }),
    { scrollIntoView: true, userEvent: 'input.format' },
  );
}
