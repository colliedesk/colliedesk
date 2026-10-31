import { useRef } from 'react';
import type { FocusEvent, KeyboardEvent, ReactNode } from 'react';
import { ChevronDownIcon } from '../../ui/icons';

interface ActionMenuProps<T> {
  label: string;
  icon?: ReactNode;
  // In place of the icon: what the closed menu shows (a value, as a select would).
  summary?: ReactNode;
  // Ids of the visible words that name the menu; then it carries no aria-label and no tip of its own.
  labelledBy?: string;
  onToggle?: (open: boolean) => void;
  disabled: boolean;
  children: (apply: (value: T) => void) => ReactNode;
  onApply: (value: T) => void;
}

// A menu of choices that each apply at once. A button per choice, not a select: a select fires on
// every arrow key in some browsers, and one stray key would rewrite fifty conversations.
export function ActionMenu<T>({ label, icon, summary, labelledBy, disabled, children, onApply, onToggle }: ActionMenuProps<T>) {
  const menu = useRef<HTMLDetailsElement>(null);
  const apply = (value: T) => {
    if (menu.current) {
      menu.current.open = false;
      // The pressed button is about to be hidden; keep focus on the menu that opened it.
      menu.current.querySelector('summary')?.focus();
    }
    onApply(value);
  };
  // Escape closes an open menu and stops there; a bar around it may treat a second Escape as its own.
  const closeOnEscape = (event: KeyboardEvent<HTMLDetailsElement>) => {
    if (event.key !== 'Escape' || !menu.current?.open) return;
    event.stopPropagation();
    menu.current.open = false;
    menu.current.querySelector('summary')?.focus();
  };
  const closeOnFocusOut = (event: FocusEvent<HTMLDetailsElement>) => {
    if (menu.current?.open && !menu.current.contains(event.relatedTarget as Node | null)) menu.current.open = false;
  };
  return (
    <details className="action-menu" ref={menu} onKeyDown={closeOnEscape} onBlur={closeOnFocusOut} data-disabled={disabled || undefined}
      onToggle={onToggle && ((event) => onToggle(event.currentTarget.open))}>
      <summary aria-label={labelledBy ? undefined : label} aria-labelledby={labelledBy} data-tip={labelledBy ? undefined : label} aria-disabled={disabled} onClick={(event) => { if (disabled) event.preventDefault(); }}>
        {summary ?? icon}<ChevronDownIcon />
      </summary>
      <div className="action-popover" role="group" aria-label={label}>{children(apply)}</div>
    </details>
  );
}
