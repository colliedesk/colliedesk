import { useEffect, useId, useRef, useState, useSyncExternalStore } from 'react';
import type { KeyboardEvent, ReactNode } from 'react';
import { dialogKeys, keepDialogFocus, useReturnFocus } from '../../ui/dialog-keys';
import { Icon } from './editor/icons';
import type { IconName } from './editor/icons';
import { kbMessages } from './kb-messages';
import { mediaMessages } from './media-messages';

// The pieces the knowledge base pages share: dialogs that are bottom sheets on a phone, menus, toasts.
// Rule of these pages: no action sits outside the window or inside an area that has to be scrolled to
// reach it; only content (rows, cards, the text) scrolls. A phone gets its page actions in a bar at the
// bottom, its menus and settings as sheets.

// The width under which the pages take their phone layout (layout.css changes the shell there too).
const phoneQuery = '(max-width: 44rem)';
const subscribePhone = (change: () => void) => {
  const query = window.matchMedia?.(phoneQuery);
  query?.addEventListener('change', change);
  return () => query?.removeEventListener('change', change);
};
const isPhone = () => window.matchMedia?.(phoneQuery).matches ?? false;
export const usePhone = () => useSyncExternalStore(subscribePhone, isPhone);

interface DialogProps {
  title: ReactNode;
  role?: 'dialog' | 'alertdialog';
  // sheet: a bottom sheet on a phone (a centred dialog elsewhere); full: the whole screen on a phone.
  phone?: 'sheet' | 'full';
  className?: string;
  footer?: ReactNode;
  describedBy?: string;
  // closeLabel puts a Close button in the header.
  closeLabel?: string;
  // returnFocus: false when the one who closes it puts the focus somewhere itself (the editor's text).
  returnFocus?: boolean;
  // fieldFirst false starts at the header's Close rather than at a field (a phone keyboard would cover the screen).
  fieldFirst?: boolean;
  children: ReactNode;
  onClose: () => void;
}

// KBDialog is a modal: Escape and the backdrop close it, Tab stays inside, and the focus goes back where it was.
export function KBDialog({ title, role = 'dialog', phone = 'sheet', className, footer, describedBy, closeLabel, returnFocus = true, fieldFirst = true, children, onClose }: DialogProps) {
  const id = useId();
  const root = useRef<HTMLDivElement>(null);
  useReturnFocus(undefined, returnFocus);
  useEffect(() => {
    // The first field, or else the first button of the footer (Cancel in a confirmation), takes the focus.
    const box = root.current;
    const first = box?.querySelector<HTMLElement>('[data-autofocus]')
      ?? (fieldFirst ? box?.querySelector<HTMLElement>('.kb-dialog-body input:not([type=hidden]), .kb-dialog-body textarea') : null)
      ?? box?.querySelector<HTMLElement>(fieldFirst ? '.kb-dialog-foot button, .kb-dialog-head button' : '.kb-dialog-head button');
    first?.focus();
  }, []);
  return (
    <div className="dialog-backdrop kb-backdrop" data-phone={phone} onMouseDown={(event) => { keepDialogFocus(event); if (event.target === event.currentTarget) onClose(); }}>
      <div ref={root} className={`kb-dialog${className ? ` ${className}` : ''}`} role={role} aria-modal="true" aria-labelledby={`${id}-title`} aria-describedby={describedBy}
        onKeyDown={(event) => {
          // A dialog over a panel closes alone: the panel under it does not hear its Escape.
          if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); }
          dialogKeys(root, onClose)(event);
        }}>
        <span className="kb-grab" aria-hidden="true" />
        <header className="kb-dialog-head">
          <h2 id={`${id}-title`}>{title}</h2>
          {closeLabel && <button type="button" className="icon-button" aria-label={closeLabel} onClick={onClose}><Icon name="close" /></button>}
        </header>
        <div className="kb-dialog-body">{children}</div>
        {footer && <footer className="kb-dialog-foot">{footer}</footer>}
      </div>
    </div>
  );
}

export interface MenuItem {
  label: string;
  // hint is a second line (a menu that explains its choices) or a note at the end.
  hint?: string;
  icon?: IconName;
  danger?: boolean;
  disabled?: boolean;
  separated?: boolean;
  onSelect: () => void;
}

interface MenuProps {
  label: string;
  // trigger draws the button; title heads the sheet on a phone.
  trigger: ReactNode;
  title: string;
  items: MenuItem[];
  className?: string;
  buttonClassName?: string;
  align?: 'start' | 'end';
  describe?: boolean;
  disabled?: boolean;
  // A menu in a toolbar joins its roving focus: its button is named by data-tool and takes the tabIndex given.
  tool?: string;
  tabIndex?: number;
  // grid lays a long menu out as tiles on a phone, so its sheet fits the screen without scrolling.
  grid?: boolean;
}

// KBMenu is a button with a menu under it; on a phone the menu is a sheet from the bottom with 48 px rows.
// Arrows move between its items, Escape closes it and gives the focus back to the button.
export function KBMenu({ label, trigger, title, items, className, buttonClassName, align = 'end', describe = false, disabled, tool, tabIndex, grid }: MenuProps) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const close = (focus = true) => { setOpen(false); if (focus) button.current?.focus(); };
  useEffect(() => {
    if (!open) return;
    menu.current?.querySelector<HTMLElement>('[role=menuitem]:not(:disabled)')?.focus();
    const outside = (event: PointerEvent) => {
      if (!menu.current?.contains(event.target as Node) && !button.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', outside);
    return () => document.removeEventListener('pointerdown', outside);
  }, [open]);
  const keys = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); return; }
    if (event.key === 'Tab') { setOpen(false); return; }
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const all = [...(menu.current?.querySelectorAll<HTMLElement>('[role=menuitem]:not(:disabled)') ?? [])];
    const at = all.indexOf(document.activeElement as HTMLElement);
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? all.length - 1 : (at + (event.key === 'ArrowDown' ? 1 : all.length - 1)) % all.length;
    all[next]?.focus();
  };
  return (
    <div className={`kb-menu-wrap${className ? ` ${className}` : ''}`}>
      <button ref={button} type="button" className={buttonClassName} aria-label={label} aria-haspopup="menu" aria-expanded={open} aria-controls={open ? `${id}-menu` : undefined}
        disabled={disabled} data-tool={tool} tabIndex={tabIndex} onMouseDown={tool ? (event) => event.preventDefault() : undefined} onClick={() => setOpen(!open)}>{trigger}</button>
      {open && <>
        <div className="kb-menu-scrim" aria-hidden="true" onPointerDown={() => close(false)} />
        <div ref={menu} id={`${id}-menu`} className="kb-menu" data-align={align} data-describe={describe ? 'true' : undefined} data-grid={grid ? 'true' : undefined} role="menu" aria-label={title} onKeyDown={keys}>
          <span className="kb-grab" aria-hidden="true" />
          <div className="kb-menu-head"><span>{title}</span>
            <button type="button" className="icon-button" aria-label={kbMessages.close} onClick={() => close()}><Icon name="close" /></button></div>
          {items.map((item) => (
            <div key={item.label} className="kb-menu-item" data-separated={item.separated ? 'true' : undefined}>
              <button type="button" role="menuitem" className={item.danger ? 'danger-action' : undefined} disabled={item.disabled}
                onClick={() => { close(!describe); item.onSelect(); }}>
                {item.icon && <Icon name={item.icon} />}
                <span className="kb-menu-text"><span>{item.label}</span>{item.hint && <small>{item.hint}</small>}</span>
              </button>
            </div>
          ))}
        </div>
      </>}
    </div>
  );
}

// KBToast floats at the foot of the window and never pushes the page: what the last action did, with its Undo.
export function KBToast({ text, undo, onUndo, onDismiss }: { text: string; undo?: string; onUndo?: () => void; onDismiss: () => void }) {
  return (
    <div className="kb-toast" role="status">
      <span>{text}</span>
      {undo && onUndo && <button type="button" onClick={onUndo}>{undo}</button>}
      <button type="button" className="kb-toast-close" aria-label={kbMessages.dismiss} onClick={onDismiss}><Icon name="close" /></button>
    </div>
  );
}

interface HeadProps {
  current: 'articles' | 'media';
  heading: string;
  // articles counts the live articles, from the collections; the Media tab has no count the API gives.
  articles?: number;
  actions?: ReactNode;
}

// KBHead is the one row at the top of the knowledge base pages: the page title, Articles | Media, and the page's actions.
export function KBHead({ current, heading, articles, actions }: HeadProps) {
  return (
    <header className="kb-head">
      <h1 id="kb-heading" className="kb-page-title">{heading}</h1>
      <nav className="kb-sections" aria-label={mediaMessages.sections}>
        <a href="/knowledge" aria-current={current === 'articles' ? 'page' : undefined}>
          {mediaMessages.articlesTab}{articles !== undefined && <span className="kb-count-tag">{articles.toLocaleString('en')}</span>}</a>
        <a href="/knowledge/media" aria-current={current === 'media' ? 'page' : undefined}>{mediaMessages.mediaTab}</a>
      </nav>
      {actions && <div className="kb-head-actions">{actions}</div>}
    </header>
  );
}
