import { useEffect, useId, useRef, useState } from 'react';
import type { KeyboardEvent, Ref } from 'react';
import './row-menu.css';

export interface RowMenuItem {
  key: string;
  label: string;
  // name is what a screen reader says when it differs from the words shown (it starts with them).
  name?: string;
  danger?: boolean;
  onSelect: () => void;
}

// RowMenu is the "⋯" button at the end of a row with the row's actions under it. Enter or Space on the button opens
// it with the first action focused; the arrow keys, Home and End move; Escape closes it and returns the focus to the
// button; Tab or a press outside closes it. While it is open it is in the page as role="menu", which is how Esc is
// kept from leaving Settings (back-to-app.tsx).
export function RowMenu({ label, items, buttonRef }: { label: string; items: RowMenuItem[]; buttonRef?: Ref<HTMLButtonElement> }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement | null>(null);
  const menu = useRef<HTMLDivElement>(null);
  const name = `Actions for ${label}`;
  const entries = () => [...(menu.current?.querySelectorAll<HTMLElement>('[role=menuitem]') ?? [])];
  useEffect(() => {
    if (!open) return undefined;
    entries()[0]?.focus();
    const outside = (event: PointerEvent) => {
      if (!menu.current?.contains(event.target as Node) && !button.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', outside);
    return () => document.removeEventListener('pointerdown', outside);
  }, [open]);
  const keys = (event: KeyboardEvent) => {
    const list = entries();
    const at = list.indexOf(document.activeElement as HTMLElement);
    const move = (index: number) => { event.preventDefault(); list[(index + list.length) % list.length]?.focus(); };
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setOpen(false); button.current?.focus(); }
    else if (event.key === 'Tab') setOpen(false);
    else if (event.key === 'ArrowDown') move(at + 1);
    else if (event.key === 'ArrowUp') move(at - 1);
    else if (event.key === 'Home') move(0);
    else if (event.key === 'End') move(list.length - 1);
  };
  return <div className="row-menu-wrap">
    <button ref={(node) => {
      button.current = node;
      if (typeof buttonRef === 'function') buttonRef(node); else if (buttonRef) (buttonRef as { current: HTMLButtonElement | null }).current = node;
    }} type="button" className="icon-button row-menu-button" aria-label={name} aria-haspopup="menu" aria-expanded={open}
    aria-controls={open ? id : undefined} onClick={() => setOpen(!open)}>
      <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true"><circle cx="5" cy="12" r="1.6" /><circle cx="12" cy="12" r="1.6" /><circle cx="19" cy="12" r="1.6" /></svg>
    </button>
    {open && <div ref={menu} id={id} className="row-menu" role="menu" aria-label={name} onKeyDown={keys}>
      {items.map((item) => <button key={item.key} type="button" role="menuitem" tabIndex={-1} className={item.danger ? 'danger-action' : undefined} aria-label={item.name}
        onClick={() => { setOpen(false); item.onSelect(); }}>{item.label}</button>)}
    </div>}
  </div>;
}
