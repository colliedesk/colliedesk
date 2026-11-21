import { useEffect, useRef, useState } from 'react';
import { EditorState, Prec } from '@codemirror/state';
import type { Extension } from '@codemirror/state';
import { EditorView, drawSelection, dropCursor, highlightSpecialChars, keymap, placeholder } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, redo, undo } from '@codemirror/commands';
import { openSearchPanel, search, searchKeymap } from '@codemirror/search';
import { kbMessages } from '../kb-messages';
import { activeOf, callout, codeBlock, inlineCode, insertBlock, insertLink, sameActive, setHeading, tableTemplate, toggleLines, toggleMark } from './commands';
import { insertArticleLink } from './article-link';
import { setBlockFormat, setColor, toggleWrap } from './format-commands';
import { markdownHighlight, markdownLanguage } from './theme';
import { startUpload, uploadEvents, uploads as uploadField, uploadsPending } from './upload';
import { pasteEvents } from './paste-formatted';
import type { Pasted } from './paste-formatted';
import { PasteToast } from './paste-toast';
import { EditorFrame } from './editor-frame';
import type { Active, EditorProps, MarkKind, ToolTarget, UploadHooks } from './editor-types';

export type { EditorHandle, UploadHooks, ViewMode } from './editor-types';
export type CodeMirrorEditorProps = EditorProps;

const formatKeys = Prec.high(keymap.of([
  // defaultKeymap binds Mod-i to selectParentSyntax; this one comes first.
  { key: 'Mod-b', run: (view) => (toggleMark(view, '**'), true) },
  { key: 'Mod-i', run: (view) => (toggleMark(view, '*'), true) },
  { key: 'Mod-u', run: (view) => (toggleWrap(view, '<u>', '</u>'), true) },
  { key: 'Mod-e', run: (view) => (inlineCode(view), true) },
  { key: 'Mod-k', run: (view) => (insertLink(view), true) },
]));
// The find and replace panel in the app's words (CodeMirror's own are lowercase).
const phrases = EditorState.phrases.of({
  Find: 'Find', Replace: 'Replace', next: 'Next', previous: 'Previous', all: 'All', 'match case': 'Match case', regexp: 'Regular expression',
  'by word': 'Whole words', replace: 'Replace', 'replace all': 'Replace all', close: 'Close',
});
const marks: Record<Exclude<MarkKind, 'code' | 'underline' | 'highlight'>, string> = { bold: '**', italic: '*', strike: '~~' };
const wraps = { underline: ['<u>', '</u>'], highlight: ['<mark>', '</mark>'] } as const;

// sourceTarget is what the toolbar does to the Markdown source: it adds or takes away syntax, never parses.
function sourceTarget(view: EditorView, hooks: { current: UploadHooks }): ToolTarget {
  return {
    mark: (kind) => (kind === 'code' ? inlineCode(view) : kind === 'underline' || kind === 'highlight' ? toggleWrap(view, wraps[kind][0], wraps[kind][1]) : toggleMark(view, marks[kind])),
    color: (color) => setColor(view, color),
    align: (align) => setBlockFormat(view, { align }),
    indent: (delta) => setBlockFormat(view, { indent: delta }),
    link: () => insertLink(view),
    block: (block) => setHeading(view, block),
    lines: (kind) => toggleLines(view, kind),
    insert: (markdown, select) => insertBlock(view, markdown, select),
    callout: (type) => insertBlock(view, ...callout(type)),
    codeBlock: (language) => insertBlock(view, ...codeBlock(language)),
    table: () => insertBlock(view, tableTemplate, [2, 8]),
    rule: () => insertBlock(view, '---'),
    articleLink: (article) => insertArticleLink(view, article),
    upload: (files) => startUpload(view, files, view.state.selection.main.head, hooks.current),
    search: () => openSearchPanel(view),
    undo: () => undo(view),
    redo: () => redo(view),
    focus: () => view.focus(),
  };
}

// CodeMirrorEditor is the article's text as Markdown source: what is typed is what body_md stores, byte for byte
// (line endings aside: CodeMirror writes \n for \r\n and \r, and the page does not count that as a change).
// CodeMirror 6 with the bare @lezer/markdown GFM parser for highlighting (not @codemirror/lang-markdown, which
// brings the HTML, CSS and JavaScript languages along), history and search.
export default function CodeMirrorEditor({ initial, uploads, label, describedBy, invalid, mode, articleId, preview, onChange, onReady }: EditorProps) {
  const host = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | undefined>(undefined);
  const [target, setTarget] = useState<ToolTarget>();
  // pasted: the last formatted paste, offered as plain text until the person types on.
  const [pasted, setPasted] = useState<Pasted>();
  const [active, setActive] = useState<Active>();
  // The view is made once; what its callbacks call reads the latest props through this.
  const latest = useRef({ uploads, onChange, onReady, initial, label, describedBy });
  useEffect(() => { latest.current = { uploads, onChange, onReady, initial, label, describedBy }; });
  const hooks = useRef<UploadHooks>(uploads);
  useEffect(() => { hooks.current = uploads; }, [uploads]);
  useEffect(() => {
    if (invalid) viewRef.current?.contentDOM.setAttribute('aria-invalid', 'true');
    else viewRef.current?.contentDOM.removeAttribute('aria-invalid');
  }, [invalid, active]);

  useEffect(() => {
    let alive = true;
    let timer: number | undefined;
    // emitted is the text the page last heard of, so a load or an undo back to it is not sent as a change. It is
    // the text as CodeMirror holds it: line endings \r\n and \r come out as \n, which is no edit.
    let emitted = '';
    const emit = (view: EditorView) => {
      window.clearTimeout(timer);
      const markdown = view.state.doc.toString();
      if (markdown !== emitted) { emitted = markdown; latest.current.onChange(markdown); }
      return markdown;
    };
    const build = (): Extension[] => [
      EditorView.lineWrapping, history(), drawSelection(), dropCursor(), highlightSpecialChars(), formatKeys,
      keymap.of([...searchKeymap, ...defaultKeymap, ...historyKeymap]), search({ top: true }), markdownLanguage, markdownHighlight,
      placeholder(kbMessages.writePlaceholder), phrases,
      EditorView.contentAttributes.of({ 'aria-label': latest.current.label, 'aria-describedby': latest.current.describedBy, 'aria-multiline': 'true', spellcheck: 'true' }),
      uploadField, uploadEvents(hooks), pasteEvents(hooks, (next) => { if (alive) setPasted(next); }),
      EditorView.updateListener.of((update) => {
        if (!alive) return;
        if (update.docChanged || update.selectionSet || update.transactions.length) {
          const next = activeOf(update.state);
          setActive((current) => (sameActive(current, next) ? current : next));
        }
        if (!update.docChanged) return;
        if (!update.transactions.some((tr) => tr.isUserEvent('input.paste'))) setPasted(undefined);
        window.clearTimeout(timer);
        timer = window.setTimeout(() => { if (alive) emit(update.view); }, 120);
      }),
    ];
    const view = new EditorView({ parent: host.current!, state: EditorState.create({ doc: latest.current.initial, extensions: build() }) });
    viewRef.current = view;
    emitted = view.state.doc.toString();
    setActive(activeOf(view.state));
    setTarget(sourceTarget(view, hooks));
    latest.current.onReady({
      markdown: () => emit(view),
      load: (markdown) => {
        window.clearTimeout(timer);
        view.setState(EditorState.create({ doc: markdown, extensions: build() }));
        setPasted(undefined);
        view.scrollDOM.scrollTop = 0;
        emitted = view.state.doc.toString();
        setActive(activeOf(view.state));
      },
      uploading: () => uploadsPending(view),
      focus: () => view.focus(),
      replace: (find, by) => {
        const at = view.state.doc.toString().indexOf(find);
        if (at < 0) return false;
        view.dispatch({ changes: { from: at, to: at + find.length, insert: by }, userEvent: 'input.alt' });
        emit(view);
        return true;
      },
      insert: (markdown) => {
        // The block's own trailing line ends are dropped: insertBlock sets the blank lines around it.
        insertBlock(view, markdown.replace(/\n+$/, ''));
        view.focus();
        emit(view);
      },
    });
    return () => {
      alive = false;
      window.clearTimeout(timer);
      viewRef.current = undefined;
      view.destroy();
    };
  }, []);

  return (
    <EditorFrame mode={mode} articleId={articleId} preview={preview} target={target} active={active}
      overlay={pasted && <PasteToast pasted={pasted} view={viewRef.current} onChange={setPasted} />}>
      <div ref={host} className="cm-host" hidden={mode === 'preview'} />
    </EditorFrame>
  );
}
