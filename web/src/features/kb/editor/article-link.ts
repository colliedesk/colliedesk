import { EditorSelection } from '@codemirror/state';
import type { EditorView } from '@codemirror/view';

// articleAddress is the part of an article's public address that stays the same on every host of the help center:
// /a/<number>-<slug>. The help center puts its own path in front when it shows the article, so a link written this
// way keeps working when the help center moves.
export function articleAddress(url: string): string {
  const { pathname } = new URL(url);
  const at = pathname.lastIndexOf('/a/');
  return at < 0 ? pathname : pathname.slice(at);
}

// A title is plain text: every Markdown sign in it is escaped so it shows as written.
const escapeTitle = (text: string) => text.replace(/[\\[\]*_`<>]/g, (char) => `\\${char}`);

// A selection is Markdown already: only a bare bracket, which would end the link text early, is escaped;
// escapes and code spans stay as written.
const escapeSelection = (text: string) => text.replace(/(`+)[\s\S]*?\1|\\[\s\S]|[[\]]/g, (part) => part.length === 1 ? `\\${part}` : part);

// insertArticleLink writes [text](/a/<n>-slug) for each selection: the selected text is the link text, or the
// article's title when nothing is selected. The cursor ends after the link.
export function insertArticleLink(view: EditorView, article: { title: string; url: string }) {
  const { state } = view;
  const address = articleAddress(article.url);
  view.dispatch(
    state.changeByRange((range) => {
      const selected = state.sliceDoc(range.from, range.to);
      // Right after a bare !, [text](address) would be an image.
      const bang = state.sliceDoc(range.from - 1, range.from) === '!' && state.sliceDoc(range.from - 2, range.from - 1) !== '\\' ? '\\' : '';
      const insert = `[${selected ? escapeSelection(selected) : escapeTitle(article.title)}](${address})`;
      const changes = [...(bang ? [{ from: range.from - 1, insert: bang }] : []), { from: range.from, to: range.to, insert }];
      return { changes, range: EditorSelection.cursor(range.from + bang.length + insert.length) };
    }),
    { scrollIntoView: true, userEvent: 'input.format' },
  );
}
