import { useEffect, useState } from 'react';
import type { KeyboardEvent, MouseEvent, RefObject } from 'react';

const FOCUSABLE = 'button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), summary';

// dialogKeys handles the keys of a modal dialog: Escape closes it, and Tab wraps so the page behind the
// dialog is not reached from the keyboard. Controls inside a hidden part (a tab not shown) are not reachable, so
// they are neither the first nor the last.
export function dialogKeys(root: RefObject<HTMLElement | null>, onClose: () => void) {
  return (event: KeyboardEvent) => {
    if (event.key === 'Escape') { onClose(); return; }
    if (event.key !== 'Tab') return;
    const items = Array.from(root.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? []).filter((item) => !item.closest('[hidden]'))
      .sort((a, b) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1));
    const first = items[0];
    const last = items[items.length - 1];
    if (!first || !last) return;
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  };
}

// keepDialogFocus goes on a dialog's backdrop: a press on the backdrop itself would move the focus to the
// page behind, where the dialog's keys (Escape, Tab) no longer reach it. A press inside the dialog is left
// alone, so a field still takes the caret where it lands.
export function keepDialogFocus(event: MouseEvent) {
  if (event.target === event.currentTarget) event.preventDefault();
}

// useReturnFocus gives focus back, when a dialog closes, to what had it when the dialog opened (the
// button that opened it), so keyboard and screen reader users keep their place on the page. Safari does
// not focus a button on click, so opener, when given, names that button in the page instead. active false leaves
// the focus to whoever closes the dialog (an editor that takes it back itself).
export function useReturnFocus(opener?: string, active = true) {
  // Read on the first render, before an autoFocus field inside the dialog takes the focus.
  const [focused] = useState(() => (document.activeElement instanceof HTMLElement && document.activeElement !== document.body ? document.activeElement : null));
  useEffect(() => () => {
    if (!active) return;
    const target = focused?.isConnected ? focused : opener ? document.querySelector<HTMLElement>(opener) : null;
    target?.focus();
  }, [focused, opener, active]);
}
