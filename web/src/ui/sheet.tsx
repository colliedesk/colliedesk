import { useEffect, useId, useRef } from 'react';
import type { KeyboardEvent, ReactNode } from 'react';
import { dialogKeys, tabbables, useReturnFocus } from './dialog-keys';
import { XIcon } from './icons';
import './sheet.css';

// Sheet is a panel that opens beside a list to set one row of it: it holds the focus (Tab stays inside), Escape and
// a press on the page behind close it, and focus goes back to the row that opened it. A dialog opened from inside it
// (a confirmation) stacks above, since the sheet is not a dialog's backdrop.
export function Sheet({ title, onClose, closeLabel, children, footer }: {
  title: string; onClose: () => void; closeLabel: string; children: ReactNode; footer: ReactNode;
}) {
  useReturnFocus();
  const root = useRef<HTMLDivElement>(null);
  const titleId = useId();
  useEffect(() => { root.current?.focus(); }, []);
  const closing = dialogKeys(root, onClose);
  // Escape first closes a tip or tooltip that is open, and only then the sheet.
  const keys = (event: KeyboardEvent) => {
    if (event.key === 'Escape' && document.querySelector('.tooltip:not([hidden])')) return;
    // The sheet itself holds the focus when it opens: Tab goes to its first control and Shift+Tab to its last, not to the page behind.
    if (event.key === 'Tab' && document.activeElement === root.current) {
      const items = tabbables(root);
      event.preventDefault();
      (event.shiftKey ? items[items.length - 1] : items[0])?.focus();
      return;
    }
    closing(event);
  };
  return (
    <div className="sheet-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <div className="sheet" role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1} ref={root} onKeyDown={keys}>
        <header><h2 id={titleId}>{title}</h2>
          <button type="button" className="icon-button" aria-label={closeLabel} onClick={onClose}><XIcon /></button></header>
        <div className="sheet-body">{children}</div>
        <footer>{footer}</footer>
      </div>
    </div>
  );
}
