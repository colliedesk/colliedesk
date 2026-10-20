import { useEffect, useId, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent, ReactNode } from 'react';
import { EditorState, Prec } from '@codemirror/state';
import type { Extension } from '@codemirror/state';
import { EditorView, drawSelection, dropCursor, highlightSpecialChars, keymap, placeholder } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, redo, undo } from '@codemirror/commands';
import { openSearchPanel, search, searchKeymap } from '@codemirror/search';
import { kbMessages } from '../kb-messages';
import { activeOf, codeBlockTemplate, inlineCode, insertBlock, insertLink, sameActive, setHeading, tableTemplate, toggleLines, toggleMark } from './commands';
import type { Active, Block, LineKind } from './commands';
import { Icon } from './icons';
import type { IconName } from './icons';
import { markdownHighlight, markdownLanguage } from './theme';
import { embedSnippet, parseVideoLink } from '../video-link';
import { startUpload, uploadEvents, uploads as uploadField, uploadsPending } from './upload';
import type { UploadHooks } from './upload';
import './editor.css';

export type { UploadHooks };

// EditorHandle is what the article page asks of the editor.
export interface EditorHandle {
  // markdown is the text as it would be saved, now (onChange runs a moment after typing stops).
  markdown(): string;
  // load replaces the text, with a fresh undo history.
  load(markdown: string): void;
  uploading(): boolean;
  focus(): void;
}

export type ViewMode = 'edit' | 'split' | 'preview';

export interface CodeMirrorEditorProps {
  initial: string;
  uploads: UploadHooks;
  label: string;
  describedBy: string;
  invalid: boolean;
  mode: ViewMode;
  // preview is shown beside the text (split) or instead of it (preview).
  preview?: ReactNode;
  onChange: (markdown: string) => void;
  onReady: (handle: EditorHandle) => void;
}

const formatKeys = Prec.high(keymap.of([
  // defaultKeymap binds Mod-i to selectParentSyntax; this one comes first.
  { key: 'Mod-b', run: (view) => (toggleMark(view, '**'), true) },
  { key: 'Mod-i', run: (view) => (toggleMark(view, '*'), true) },
  { key: 'Mod-e', run: (view) => (inlineCode(view), true) },
  { key: 'Mod-k', run: (view) => (insertLink(view), true) },
]));
// The find and replace panel in the app's words (CodeMirror's own are lowercase).
const phrases = EditorState.phrases.of({
  Find: 'Find', Replace: 'Replace', next: 'Next', previous: 'Previous', all: 'All', 'match case': 'Match case', regexp: 'Regular expression',
  'by word': 'Whole words', replace: 'Replace', 'replace all': 'Replace all', close: 'Close',
});
const mod = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.userAgent) ? '⌘' : 'Ctrl+';

// CodeMirrorEditor is the article's text as Markdown source: what is typed is what body_md stores, byte for byte
// (line endings aside: CodeMirror writes \n for \r\n and \r, and the page does not count that as a change).
// CodeMirror 6 with the bare @lezer/markdown GFM parser for highlighting (not @codemirror/lang-markdown, which
// brings the HTML, CSS and JavaScript languages along), history and search.
export default function CodeMirrorEditor({ initial, uploads, label, describedBy, invalid, mode, preview, onChange, onReady }: CodeMirrorEditorProps) {
  const id = useId();
  const host = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | undefined>(undefined);
  const fileInput = useRef<HTMLInputElement>(null);
  const videoLink = useRef<HTMLInputElement>(null);
  // video is the Insert video row: closed, open, or open with why the link cannot be used.
  const [video, setVideo] = useState<{ error: string } | undefined>();
  const [active, setActive] = useState<Active>();
  // rover is the toolbar button that is the Tab stop; when it is disabled (Undo with nothing to undo), the first
  // button is, so the toolbar never loses its stop.
  const [rover, setRover] = useState<string>('bold');
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
      uploadField, uploadEvents(hooks),
      EditorView.updateListener.of((update) => {
        if (!alive) return;
        if (update.docChanged || update.selectionSet || update.transactions.length) {
          const next = activeOf(update.state);
          setActive((current) => (sameActive(current, next) ? current : next));
        }
        if (!update.docChanged) return;
        window.clearTimeout(timer);
        timer = window.setTimeout(() => { if (alive) emit(update.view); }, 120);
      }),
    ];
    const view = new EditorView({ parent: host.current!, state: EditorState.create({ doc: latest.current.initial, extensions: build() }) });
    viewRef.current = view;
    emitted = view.state.doc.toString();
    setActive(activeOf(view.state));
    latest.current.onReady({
      markdown: () => emit(view),
      load: (markdown) => {
        window.clearTimeout(timer);
        view.setState(EditorState.create({ doc: markdown, extensions: build() }));
        view.scrollDOM.scrollTop = 0;
        emitted = view.state.doc.toString();
        setActive(activeOf(view.state));
      },
      uploading: () => uploadsPending(view),
      focus: () => view.focus(),
    });
    return () => {
      alive = false;
      window.clearTimeout(timer);
      viewRef.current = undefined;
      view.destroy();
    };
  }, []);

  const act = (run: (view: EditorView) => unknown) => {
    const view = viewRef.current;
    if (!view) return;
    run(view);
    view.focus();
  };
  // Roving focus: the toolbar is one Tab stop, arrows (and Home, End) move between its buttons.
  const onToolbarKey = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    const buttons = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('button[data-tool]:not(:disabled)')];
    const at = buttons.indexOf(document.activeElement as HTMLButtonElement);
    if (at < 0) return;
    event.preventDefault();
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (at + (event.key === 'ArrowRight' ? 1 : buttons.length - 1)) % buttons.length;
    setRover(buttons[next].dataset.tool ?? 'bold');
    buttons[next].focus();
  };

  useEffect(() => {
    if (video && !video.error) videoLink.current?.focus();
  }, [video]);
  const closeVideo = () => {
    setVideo(undefined);
    viewRef.current?.focus();
  };
  // insertVideo puts the player of a YouTube or Vimeo link in a block of its own, or says why it cannot.
  const insertVideo = () => {
    const parsed = parseVideoLink(videoLink.current?.value ?? '');
    if ('error' in parsed) {
      setVideo({ error: parsed.error });
      videoLink.current?.focus();
      return;
    }
    setVideo(undefined);
    act((view) => insertBlock(view, embedSnippet(parsed)));
  };

  const editing = mode !== 'preview' && !!active;
  const stop = (rover === 'undo' && !active?.canUndo) || (rover === 'redo' && !active?.canRedo) ? 'bold' : rover;
  const t = kbMessages.tools;
  const tool = (name: IconName, text: string, keys: string, run: (view: EditorView) => unknown, pressed?: boolean, disabled?: boolean) => (
    <button type="button" data-tool={name} className="md-tool" aria-label={`${text}${keys ? ` (${keys})` : ''}`} data-tip={text} aria-pressed={pressed}
      disabled={disabled || !editing} tabIndex={stop === name ? 0 : -1} onMouseDown={(event) => event.preventDefault()} onClick={() => act(run)}>
      <Icon name={name} />
    </button>
  );
  const line = (kind: LineKind) => (view: EditorView) => toggleLines(view, kind);
  return (
    <div className="md-shell" data-mode={mode}>
      <div className="md-toolbar scroll-x-fade" role="toolbar" aria-label={kbMessages.formatting} onKeyDown={onToolbarKey}>
        <label className="sr-only" htmlFor={`${id}-block`}>{kbMessages.blockType}</label>
        <select id={`${id}-block`} className="md-block" value={active?.block ?? 'paragraph'} disabled={!editing}
          onChange={(event) => act((view) => setHeading(view, event.target.value as Block))}>
          <option value="paragraph">{t.paragraph}</option>
          <option value="h1">{t.h1}</option>
          <option value="h2">{t.h2}</option>
          <option value="h3">{t.h3}</option>
        </select>
        <span className="md-sep" aria-hidden="true" />
        {tool('bold', t.bold, `${mod}B`, (view) => toggleMark(view, '**'), active?.strong)}
        {tool('italic', t.italic, `${mod}I`, (view) => toggleMark(view, '*'), active?.emphasis)}
        {tool('strike', t.strike, '', (view) => toggleMark(view, '~~'))}
        {tool('code', t.code, `${mod}E`, inlineCode, active?.code)}
        {tool('link', t.link, `${mod}K`, insertLink, active?.link)}
        <span className="md-sep" aria-hidden="true" />
        {tool('bullet', t.bullet, '', line('bullet'), active?.bullet)}
        {tool('ordered', t.ordered, '', line('ordered'), active?.ordered)}
        {tool('task', t.task, '', line('task'), active?.task)}
        {tool('quote', t.quote, '', line('quote'), active?.quote)}
        {tool('codeBlock', t.codeBlock, '', (view) => insertBlock(view, codeBlockTemplate, [4, 8]))}
        {tool('table', t.table, '', (view) => insertBlock(view, tableTemplate, [2, 8]))}
        {tool('hr', t.hr, '', (view) => insertBlock(view, '---'))}
        <span className="md-sep" aria-hidden="true" />
        <button type="button" data-tool="insertImage" className="md-tool md-tool-text" tabIndex={stop === 'insertImage' ? 0 : -1} disabled={!editing}
          onMouseDown={(event) => event.preventDefault()} onClick={() => fileInput.current?.click()}>
          <Icon name="image" /><span>{t.insertImage}</span>
        </button>
        <button type="button" data-tool="insertVideo" className="md-tool md-tool-text" tabIndex={stop === 'insertVideo' ? 0 : -1} disabled={!editing}
          aria-expanded={!!video} aria-controls={`${id}-video`} onMouseDown={(event) => event.preventDefault()}
          onClick={() => (video ? closeVideo() : setVideo({ error: '' }))}>
          <Icon name="video" /><span>{t.insertVideo}</span>
        </button>
        <span className="md-sep" aria-hidden="true" />
        {tool('search', t.search, `${mod}F`, openSearchPanel)}
        {tool('undo', t.undo, `${mod}Z`, undo, undefined, !active?.canUndo)}
        {tool('redo', t.redo, `${mod}Shift+Z`, redo, undefined, !active?.canRedo)}
      </div>
      {video && editing && (
        <div id={`${id}-video`} className="md-video" role="group" aria-label={t.insertVideo}
          onKeyDown={(event) => { if (event.key === 'Escape') { event.preventDefault(); closeVideo(); } }}>
          <label htmlFor={`${id}-video-link`}>{kbMessages.videoLink}</label>
          <input ref={videoLink} id={`${id}-video-link`} type="url" inputMode="url" autoComplete="off" placeholder="https://www.youtube.com/watch?v=…"
            aria-invalid={video.error ? true : undefined} aria-describedby={`${id}-video-help`} onChange={() => { if (video.error) setVideo({ error: '' }); }}
            onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); insertVideo(); } }} />
          <button type="button" className="primary-action" onClick={insertVideo}>{kbMessages.insert}</button>
          <button type="button" onClick={closeVideo}>{kbMessages.cancel}</button>
          <small id={`${id}-video-help`} className={video.error ? 'md-video-help error-copy' : 'md-video-help kb-hint'} role={video.error ? 'alert' : undefined}>
            {video.error || kbMessages.videoLinkHelp}
          </small>
        </div>
      )}
      <input ref={fileInput} type="file" accept="image/png,image/jpeg,image/gif,image/webp,video/mp4,video/webm,video/quicktime,.mov" multiple hidden tabIndex={-1} aria-hidden="true"
        onChange={(event) => {
          const files = [...(event.target.files ?? [])];
          event.target.value = '';
          const view = viewRef.current;
          if (!view || files.length === 0) return;
          startUpload(view, files, view.state.selection.main.head, hooks.current);
          view.focus();
        }} />
      <div className="cm-area">
        <div ref={host} className="cm-host" hidden={mode === 'preview'} />
        {mode !== 'edit' && <section className="cm-preview kb-preview-panel" tabIndex={0} aria-label={kbMessages.preview}>{preview}</section>}
      </div>
    </div>
  );
}
