import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';

interface ListMenuProps {
  label: string;
  // count: how many filters are on, shown beside the label.
  count?: number;
  countLabel?: string;
  className?: string;
  onOpen?: () => void;
  children: ReactNode;
}

// ListMenu is one menu over the article list (Filters, Columns): it closes on Escape, when the focus leaves it, and
// when the pointer presses outside it (a button that went away, such as Reset columns, takes the focus with it).
export function ListMenu({ label, count, countLabel, className, onOpen, children }: ListMenuProps) {
  const menu = useRef<HTMLDetailsElement>(null);
  const [open, setOpen] = useState(false);
  useEffect(() => {
    if (!open) return undefined;
    const closeOnPressOutside = (event: PointerEvent) => {
      if (menu.current?.open && !menu.current.contains(event.target as Node | null)) menu.current.open = false;
    };
    document.addEventListener('pointerdown', closeOnPressOutside);
    return () => document.removeEventListener('pointerdown', closeOnPressOutside);
  }, [open]);
  const closeOnFocusOut = (event: React.FocusEvent<HTMLDetailsElement>) => {
    if (menu.current?.open && !menu.current.contains(event.relatedTarget as Node | null)) menu.current.open = false;
  };
  const closeOnEscape = (event: React.KeyboardEvent<HTMLDetailsElement>) => {
    if (event.key !== 'Escape' || !menu.current?.open) return;
    menu.current.open = false;
    menu.current.querySelector('summary')?.focus();
  };
  return (
    <details className={`filter-menu kb-list-menu${className ? ` ${className}` : ''}`} ref={menu} onKeyDown={closeOnEscape} onBlur={closeOnFocusOut}
      onToggle={(event) => { setOpen(event.currentTarget.open); if (event.currentTarget.open) onOpen?.(); }}>
      <summary data-active={count ? true : undefined}><span>{label}</span>
        {count ? <span className="filter-count" aria-label={countLabel}>{count}</span> : null}</summary>
      <div className="filter-popover kb-list-popover" role="group" aria-label={label}>{children}</div>
    </details>
  );
}
