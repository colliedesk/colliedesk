import { useEffect, useId, useRef, useState } from 'react';
import type { CSSProperties, KeyboardEvent as ReactKeyboardEvent, ReactNode } from 'react';
import { kbMessages } from '../kb-messages';
import { ArticleLinkPicker } from '../article-link-picker';
import { MediaPicker } from '../media-picker';
import { KBMenu, usePhone } from '../kb-ui';
import { embedFor } from '../media-grid';
import { Icon } from './icons';
import type { IconName } from './icons';
import { CodeBlockDialog } from './code-block-dialog';
import { VideoLinkDialog } from './video-link-dialog';
import { insertMenu, moreMenu } from './toolbar-menus';
import type { MenuContext } from './toolbar-menus';
import type { Active, Block, ToolTarget, ViewMode } from './editor-types';
import './editor.css';

export interface EditorFrameProps {
  mode: ViewMode;
  // The article being edited, left out of the list a link to an article is picked from.
  articleId?: string;
  preview?: ReactNode;
  // target is what the tools act on; undefined until the editor is made.
  target: ToolTarget | undefined;
  active: Active | undefined;
  // overlay shows between the toolbar and the text (the offer to paste as plain text).
  overlay?: ReactNode;
  // children is the editor's own surface.
  children: ReactNode;
}

// Under this width (px) the whole desktop toolbar does not fit on one row.
const compactWidth = 720;
const mod = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.userAgent) ? '⌘' : 'Ctrl+';
// A phone's toolbar has Insert, Bold, Italic, Link and More only.
const onPhone = ['insert', 'bold', 'italic', 'link', 'more'];

// EditorFrame is the toolbar, its menus and dialogs, full screen and the preview around an editor's text: the same
// for the Markdown source and the visual editor, which differ only in the target the tools act on.
export function EditorFrame({ mode, articleId, preview, target, active, overlay, children }: EditorFrameProps) {
  const id = useId();
  const fileInput = useRef<HTMLInputElement>(null);
  const [video, setVideo] = useState(false);
  const [picking, setPicking] = useState(false);
  const [coding, setCoding] = useState(false);
  // full: the text and its toolbar fill the window until the button or Escape leaves.
  const [full, setFull] = useState(false);
  // linkAt is where the list of articles to link to opens, measured from the bar's left edge: under the button
  // that opened it. Undefined while the list is closed.
  const [linkAt, setLinkAt] = useState<number>();
  const bar = useRef<HTMLDivElement>(null);
  // rover is the toolbar button that is the Tab stop.
  const [rover, setRover] = useState<string>('bold');
  const phone = usePhone();
  // compact: the toolbar of a phone, also in a column too narrow for every tool (a tablet held upright, where the
  // side column takes its share): Insert, Bold, Italic, Link, and the rest under More. Measured on the editor itself.
  const shell = useRef<HTMLDivElement>(null);
  const [narrow, setNarrow] = useState(false);
  useEffect(() => {
    const box = shell.current;
    if (!box || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(([entry]) => setNarrow((entry?.contentRect.width ?? 1000) < compactWidth));
    observer.observe(box);
    return () => observer.disconnect();
  }, []);
  const compact = phone || narrow;

  const act = (run: (target: ToolTarget) => unknown) => {
    if (!target) return;
    run(target);
    target.focus();
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

  const editing = mode !== 'preview' && !!active;
  const search = !!target?.search;
  // A phone's toolbar keeps Link to article under More formatting, so the list opens under that button.
  const openLinking = () => {
    const trigger = bar.current?.querySelector(`[data-tool="${compact ? 'more' : 'article'}"]`);
    setLinkAt(trigger && bar.current ? trigger.getBoundingClientRect().left - bar.current.getBoundingClientRect().left : 0);
  };
  // The Tab stop falls back to Bold when its button is disabled (Undo with nothing to undo) or not in this layout.
  const stop = (rover === 'undo' && !active?.canUndo) || (rover === 'redo' && !active?.canRedo) || (compact && !onPhone.includes(rover)) ? 'bold' : rover;
  const t = kbMessages.tools;
  const tool = (name: IconName, text: string, keys: string, run: (target: ToolTarget) => unknown, pressed?: boolean, disabled?: boolean) => (
    <button type="button" data-tool={name} className="md-tool" aria-label={`${text}${keys ? ` (${keys})` : ''}`} data-tip={text} aria-pressed={pressed}
      disabled={disabled || !editing} tabIndex={stop === name ? 0 : -1} onMouseDown={(event) => event.preventDefault()} onClick={() => act(run)}>
      <Icon name={name} />
    </button>
  );
  const menus: MenuContext = {
    compact, editing, full, active, search, act, openLinking,
    openCoding: () => setCoding(true), openPicking: () => setPicking(true), openDevice: () => fileInput.current?.click(),
    openVideo: () => setVideo(true), toggleFull: () => setFull(!full),
  };
  const insert = <KBMenu label={t.insertMenuLong} title={t.insertMenuLong} align="start" describe items={insertMenu(menus)} className="md-insert" buttonClassName="md-insert-button"
    tool="insert" tabIndex={stop === 'insert' ? 0 : -1} disabled={!editing}
    trigger={<><Icon name="image" /><span>{t.insertMenu}</span><span className="md-insert-long">{t.insertMenuMore}</span><Icon name="chevronDown" /></>} />;
  // Full screen works in the preview too, so its button is never disabled. A dialog, a menu or the search panel
  // takes its own Escape first (each prevents it); a plain Escape in the text leaves full screen.
  const fullscreen = (
    <button type="button" data-tool="fullscreen" className="md-tool" aria-label={`${t.fullscreen} (${t.fullscreenKeys})`} data-tip={full ? t.exitFullscreen : t.fullscreen}
      aria-pressed={full} tabIndex={stop === 'fullscreen' ? 0 : -1} onMouseDown={(event) => event.preventDefault()} onClick={() => setFull(!full)}>
      <Icon name="fullscreen" />
    </button>
  );
  const onShellKey = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Escape' || !full || event.defaultPrevented) return;
    event.preventDefault();
    setFull(false);
  };
  return (
    <div ref={shell} className="md-shell" data-mode={mode} data-compact={compact ? 'true' : undefined} data-fullscreen={full ? 'true' : undefined} onKeyDown={onShellKey}>
      {/* The bar holds the toolbar and the article list that opens over the text just under it. */}
      <div ref={bar} className="md-bar" style={linkAt === undefined ? undefined : { '--link-at': `${linkAt}px` } as CSSProperties}>
        <div className="md-toolbar" role="toolbar" aria-label={kbMessages.formatting} onKeyDown={onToolbarKey}>
          {compact && insert}
          {!compact && <>
            <label className="sr-only" htmlFor={`${id}-block`}>{kbMessages.blockType}</label>
            <select id={`${id}-block`} className="md-block" value={active?.block ?? 'paragraph'} disabled={!editing}
              onChange={(event) => act((x) => x.block(event.target.value as Block))}>
              <option value="paragraph">{t.paragraph}</option>
              {/* The article page shows the title as its h1, so the text offers no Heading 1; a stored one still shows here. */}
              {active?.block === 'h1' && <option value="h1">{t.h1}</option>}
              <option value="h2">{t.h2}</option>
              <option value="h3">{t.h3}</option>
            </select>
            <span className="md-sep" aria-hidden="true" />
          </>}
          {compact && <span className="md-grow" />}
          {tool('bold', t.bold, `${mod}B`, (x) => x.mark('bold'), active?.strong)}
          {tool('italic', t.italic, `${mod}I`, (x) => x.mark('italic'), active?.emphasis)}
          {tool('link', t.link, `${mod}K`, (x) => x.link(), active?.link)}
          {!compact && tool('article', t.articleLink, '', openLinking)}
          {!compact && <>
            <span className="md-sep" aria-hidden="true" />
            {tool('ordered', t.ordered, '', (x) => x.lines('ordered'), active?.ordered)}
            {tool('bullet', t.bullet, '', (x) => x.lines('bullet'), active?.bullet)}
            {tool('quote', t.quote, '', (x) => x.lines('quote'), active?.quote)}
            {tool('table', t.table, '', (x) => x.table())}
            <span className="md-sep" aria-hidden="true" />
            {insert}
            <span className="md-grow" />
            {fullscreen}
            {search && tool('search', t.search, `${mod}F`, (x) => x.search?.())}
            {tool('undo', t.undo, `${mod}Z`, (x) => x.undo(), undefined, !active?.canUndo)}
          </>}
          <KBMenu label={t.moreFormatting} title={t.moreFormatting} items={moreMenu(menus)} grid className="md-more" buttonClassName="md-tool" tool="more" tabIndex={stop === 'more' ? 0 : -1}
            disabled={!editing} trigger={<Icon name="more" />} />
        </div>
        {linkAt !== undefined && editing && <ArticleLinkPicker exclude={articleId}
          onPick={(article) => { setLinkAt(undefined); act((x) => x.articleLink(article)); }}
          onClose={(restoreFocus) => { setLinkAt(undefined); if (restoreFocus) target?.focus(); }} />}
      </div>
      {picking && editing && <MediaPicker onClose={() => { setPicking(false); target?.focus(); }}
        onUpload={() => { setPicking(false); fileInput.current?.click(); }}
        onInsert={(items) => { setPicking(false); act((x) => x.insert(items.map(embedFor).join('\n\n'))); }} />}
      {coding && editing && <CodeBlockDialog onClose={() => { setCoding(false); target?.focus(); }}
        onInsert={(language) => { setCoding(false); act((x) => x.codeBlock(language)); }} />}
      {video && editing && <VideoLinkDialog onClose={() => { setVideo(false); target?.focus(); }}
        onInsert={(snippet) => { setVideo(false); act((x) => x.insert(snippet)); }} />}
      <input ref={fileInput} type="file" accept="image/png,image/jpeg,image/gif,image/webp,video/mp4,video/webm,video/quicktime,.mov" multiple hidden tabIndex={-1} aria-hidden="true"
        onChange={(event) => {
          const files = [...(event.target.files ?? [])];
          event.target.value = '';
          if (!target || files.length === 0) return;
          target.upload(files);
          target.focus();
        }} />
      {editing && overlay}
      <div className="cm-area">
        {children}
        {mode !== 'edit' && <section className="cm-preview kb-preview-panel" tabIndex={0} aria-label={kbMessages.preview}>{preview}</section>}
      </div>
    </div>
  );
}
