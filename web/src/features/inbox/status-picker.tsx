import { useEffect, useId, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import { messages } from '../../messages';
import { allStatuses } from '../../lib/inbox-query';
import { CheckIcon, ChevronDownIcon } from '../../ui/icons';

interface StatusPickerProps {
  // The status on screen: open, pending, resolved, closed, spam, or all.
  status: string;
  // How many conversations each status holds under the other filters (status_counts, with all as their sum).
  counts?: Partial<Record<string, number>>;
  onPick: (status: string) => void;
}

// A link or a view may hold several statuses (open,pending): each reads as its word.
const nameOf = (value: string) => (value === allStatuses ? messages.allStatuses
  : value.split(',').map((one) => messages.statuses[one as keyof typeof messages.statuses] ?? one).join(', '));
// A count reads with its status ("Open, 6"); the visible number beside the word is hidden from a screen reader.
const spoken = (value: string, count?: number) => (count === undefined ? nameOf(value) : `${nameOf(value)}, ${count}`);

// The dot a row of the list shows for its status; Spam, All statuses and several statuses have none.
function StatusDot({ status }: { status: string }) {
  return status === allStatuses || status === 'spam' || status.includes(',') ? null : <span className="status-dot" data-status={status} aria-hidden="true" />;
}

// StatusPicker is the status the list shows, with its count, and the menu of the others. Status is the view an agent
// switches most, so it has its own control beside the order. The menu lists every status with its count (a zero reads 0),
// Spam only once it holds a conversation or is on screen, then All statuses after a line. One click picks and closes;
// the arrow keys, Home and End move, Enter or Space picks, Escape closes; the focus then returns to the picker.
export function StatusPicker({ status, counts, onPick }: StatusPickerProps) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const shown = ['open', 'pending', 'resolved', 'closed', ...(status === 'spam' || (counts?.spam ?? 0) > 0 ? ['spam'] : [])];
  const items = () => [...(menu.current?.querySelectorAll<HTMLElement>('[role=menuitemradio]') ?? [])];
  useEffect(() => {
    if (!open) return undefined;
    (items().find((item) => item.getAttribute('aria-checked') === 'true') ?? items()[0])?.focus();
    const outside = (event: PointerEvent) => {
      if (!menu.current?.contains(event.target as Node) && !button.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', outside);
    return () => document.removeEventListener('pointerdown', outside);
  }, [open]);
  const close = () => { setOpen(false); button.current?.focus(); };
  const pick = (value: string) => { close(); onPick(value); };
  const openFromKeys = (event: KeyboardEvent) => {
    if (['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(event.key) && !open) { event.preventDefault(); setOpen(true); }
  };
  const keys = (event: KeyboardEvent) => {
    const list = items();
    const at = list.indexOf(document.activeElement as HTMLElement);
    const move = (index: number) => { event.preventDefault(); list[(index + list.length) % list.length]?.focus(); };
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); }
    else if (event.key === 'Tab') setOpen(false);
    else if (event.key === 'ArrowDown') move(at + 1);
    else if (event.key === 'ArrowUp') move(at - 1);
    else if (event.key === 'Home') move(0);
    else if (event.key === 'End') move(list.length - 1);
  };
  const count = counts?.[status];
  const item = (value: string) => {
    const checked = value === status;
    return <button key={value} type="button" role="menuitemradio" aria-checked={checked} aria-label={spoken(value, counts?.[value])} tabIndex={-1} className="status-option"
      onClick={() => pick(value)} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); pick(value); } }}>
      <span className="status-dot-slot"><StatusDot status={value} /></span>
      <span className="status-label">{nameOf(value)}</span>
      <span className="status-count" aria-hidden="true">{counts?.[value] ?? ''}</span>
      <span className="status-check" aria-hidden="true">{checked && <CheckIcon />}</span>
    </button>;
  };
  return <div className="status-picker">
    <button ref={button} type="button" className="status-pick" aria-label={messages.statusPicked(spoken(status, count))} aria-haspopup="menu" aria-expanded={open}
      aria-controls={open ? id : undefined} data-open={open || undefined} onClick={() => setOpen(!open)} onKeyDown={openFromKeys}>
      <StatusDot status={status} /><span>{nameOf(status)}</span>
      {count !== undefined && <span className="status-count" aria-hidden="true">{count}</span>}
      <ChevronDownIcon />
    </button>
    {open && <div ref={menu} id={id} className="status-menu" role="menu" aria-label={messages.status} onKeyDown={keys}>
      {shown.map(item)}
      <div role="separator" className="status-menu-line" />
      {item(allStatuses)}
    </div>}
  </div>;
}
