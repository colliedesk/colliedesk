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
import { MediaPicker } from '../media-picker';
import { KBDialog, KBMenu, usePhone } from '../kb-ui';
import type { MenuItem } from '../kb-ui';
import { embedFor } from '../media-grid';
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
  // replace changes the first place the text holds find into by (an image's alt text, set from the page); false
  // when the text no longer holds it.
  replace(find: string, by: string): boolean;
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
  // video is the YouTube or Vimeo dialog: closed, open, or open with why the link cannot be used.
  const [video, setVideo] = useState<{ error: string } | undefined>();
  // picking is the media library picker, open or not.
  const [picking, setPicking] = useState(false);
  const [active, setActive] = useState<Active>();
  // rover is the toolbar button that is the Tab stop; when it is disabled (Undo with nothing to undo), the first
  // button is, so the toolbar never loses its stop.
  const [rover, setRover] = useState<string>('bold');
  const phone = usePhone();
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
      replace: (find, by) => {
        const at = view.state.doc.toString().indexOf(find);
        if (at < 0) return false;
        view.dispatch({ changes: { from: at, to: at + find.length, insert: by }, userEvent: 'input.alt' });
        emit(view);
        return true;
      },
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
  // The Tab stop falls back to Bold when its button is disabled (Undo with nothing to undo) or not in this layout
  // (a phone's toolbar has Insert, Bold, Italic, Link and More only).
  const onPhone = ['insert', 'bold', 'italic', 'link', 'more'];
  const stop = (rover === 'undo' && !active?.canUndo) || (rover === 'redo' && !active?.canRedo) || (phone && !onPhone.includes(rover)) ? 'bold' : rover;
  const t = kbMessages.tools;
  const tool = (name: IconName, text: string, keys: string, run: (view: EditorView) => unknown, pressed?: boolean, disabled?: boolean) => (
    <button type="button" data-tool={name} className="md-tool" aria-label={`${text}${keys ? ` (${keys})` : ''}`} data-tip={text} aria-pressed={pressed}
      disabled={disabled || !editing} tabIndex={stop === name ? 0 : -1} onMouseDown={(event) => event.preventDefault()} onClick={() => act(run)}>
      <Icon name={name} />
    </button>
  );
  const line = (kind: LineKind) => (view: EditorView) => toggleLines(view, kind);
  const item = (label: string, icon: IconName, run: (view: EditorView) => unknown, disabled = false): MenuItem => ({ label, icon, disabled: disabled || !editing, onSelect: () => act(run) });
  const heading = (block: Block, label: string, icon: IconName): MenuItem => ({ label, icon, disabled: !editing || active?.block === block, onSelect: () => act((view) => setHeading(view, block)) });
  // Formatting a phone has no room for, and on a computer the less used tools: in the menu at the end.
  const more: MenuItem[] = [
    ...(phone ? [heading('paragraph', t.paragraph, 'text'), heading('h1', t.h1, 'h1'), heading('h2', t.h2, 'h2'), heading('h3', t.h3, 'h3'),
      { ...item(t.ordered, 'ordered', line('ordered')), separated: true }, item(t.bullet, 'bullet', line('bullet')), item(t.quote, 'quote', line('quote')),
      item(t.table, 'table', (view) => insertBlock(view, tableTemplate, [2, 8]))] : []),
    { ...item(t.strike, 'strike', (view) => toggleMark(view, '~~')), separated: phone }, item(t.code, 'code', inlineCode), item(t.task, 'task', line('task')),
    item(t.codeBlock, 'codeBlock', (view) => insertBlock(view, codeBlockTemplate, [4, 8])), item(t.hr, 'hr', (view) => insertBlock(view, '---')),
    ...(phone ? [{ ...item(t.search, 'search', openSearchPanel), separated: true }, item(t.undo, 'undo', undo, !active?.canUndo)] : []),
    item(t.redo, 'redo', redo, !active?.canRedo),
  ];
  const insert: MenuItem[] = [
    { label: t.fromLibrary, hint: t.fromLibraryHint, icon: 'library', disabled: !editing, onSelect: () => setPicking(true) },
    { label: t.fromDevice, hint: t.fromDeviceHint, icon: 'upload', disabled: !editing, onSelect: () => fileInput.current?.click() },
    { label: t.videoLink, hint: t.videoLinkHint, icon: 'video', disabled: !editing, onSelect: () => setVideo({ error: '' }) },
  ];
  const insertMenu = <KBMenu label={t.insertMenuLong} title={t.insertMenuLong} align="start" describe items={insert} className="md-insert" buttonClassName="md-insert-button"
    tool="insert" tabIndex={stop === 'insert' ? 0 : -1} disabled={!editing}
    trigger={<><Icon name="image" /><span>{t.insertMenu}</span><span className="md-insert-long">{t.insertMenuMore}</span><Icon name="chevronDown" /></>} />;
  return (
    <div className="md-shell" data-mode={mode}>
      <div className="md-toolbar" role="toolbar" aria-label={kbMessages.formatting} onKeyDown={onToolbarKey}>
        {phone && insertMenu}
        {!phone && <>
          <label className="sr-only" htmlFor={`${id}-block`}>{kbMessages.blockType}</label>
          <select id={`${id}-block`} className="md-block" value={active?.block ?? 'paragraph'} disabled={!editing}
            onChange={(event) => act((view) => setHeading(view, event.target.value as Block))}>
            <option value="paragraph">{t.paragraph}</option>
            <option value="h1">{t.h1}</option>
            <option value="h2">{t.h2}</option>
            <option value="h3">{t.h3}</option>
          </select>
          <span className="md-sep" aria-hidden="true" />
        </>}
        {phone && <span className="md-grow" />}
        {tool('bold', t.bold, `${mod}B`, (view) => toggleMark(view, '**'), active?.strong)}
        {tool('italic', t.italic, `${mod}I`, (view) => toggleMark(view, '*'), active?.emphasis)}
        {tool('link', t.link, `${mod}K`, insertLink, active?.link)}
        {!phone && <>
          <span className="md-sep" aria-hidden="true" />
          {tool('ordered', t.ordered, '', line('ordered'), active?.ordered)}
          {tool('bullet', t.bullet, '', line('bullet'), active?.bullet)}
          {tool('quote', t.quote, '', line('quote'), active?.quote)}
          {tool('table', t.table, '', (view) => insertBlock(view, tableTemplate, [2, 8]))}
          <span className="md-sep" aria-hidden="true" />
          {insertMenu}
          <span className="md-grow" />
          {tool('search', t.search, `${mod}F`, openSearchPanel)}
          {tool('undo', t.undo, `${mod}Z`, undo, undefined, !active?.canUndo)}
        </>}
        <KBMenu label={t.moreFormatting} title={t.moreFormatting} items={more} grid className="md-more" buttonClassName="md-tool" tool="more" tabIndex={stop === 'more' ? 0 : -1}
          disabled={!editing} trigger={<Icon name="more" />} />
      </div>
      {picking && editing && <MediaPicker onClose={() => { setPicking(false); viewRef.current?.focus(); }}
        onUpload={() => { setPicking(false); fileInput.current?.click(); }}
        onInsert={(items) => { setPicking(false); act((view) => insertBlock(view, items.map(embedFor).join('\n\n'))); }} />}
      {video && editing && <KBDialog title={t.videoLink} closeLabel={kbMessages.close} returnFocus={false} onClose={closeVideo} footer={<>
        <button type="button" onClick={closeVideo}>{kbMessages.cancel}</button>
        <button type="button" className="primary-action" onClick={insertVideo}>{kbMessages.insert}</button>
      </>}>
        <div className="field md-video">
          <label htmlFor={`${id}-video-link`}>{kbMessages.videoLink}</label>
          <input ref={videoLink} id={`${id}-video-link`} type="url" inputMode="url" autoComplete="off" placeholder="https://www.youtube.com/watch?v=…"
            aria-invalid={video.error ? true : undefined} aria-describedby={`${id}-video-help`} onChange={() => { if (video.error) setVideo({ error: '' }); }}
            onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); insertVideo(); } }} />
          <small id={`${id}-video-help`} className={video.error ? 'error-copy' : 'kb-hint'} role={video.error ? 'alert' : undefined}>
            {video.error || kbMessages.videoLinkHelp}
          </small>
        </div>
      </KBDialog>}
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
