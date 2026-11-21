import { useRef } from 'react';
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

// ListMenu is one menu over the article list (Filters, Columns): it closes on Escape and when the focus leaves it.
export function ListMenu({ label, count, countLabel, className, onOpen, children }: ListMenuProps) {
  const menu = useRef<HTMLDetailsElement>(null);
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
      onToggle={(event) => { if (event.currentTarget.open) onOpen?.(); }}>
      <summary data-active={count ? true : undefined}><span>{label}</span>
        {count ? <span className="filter-count" aria-label={countLabel}>{count}</span> : null}</summary>
      <div className="filter-popover kb-list-popover" role="group" aria-label={label}>{children}</div>
    </details>
  );
}
