import { useEffect } from 'react';
import type { MouseEvent } from 'react';
import { messages } from '../../messages';
import { ArrowLeftIcon } from '../../ui/icons';

// Settings takes over the sidebar, so the page a person was on stays one press away: Back to app, and Esc. The page
// is kept for the tab (also across a reload of Settings), and only ever an address inside the app.
const key = 'colliedesk:before-settings';

export function rememberBeforeSettings(href: string): void {
  try { sessionStorage.setItem(key, href); } catch { /* storage refused: Back to app opens the inbox */ }
}

// beforeSettings is where Back to app goes: the page left for Settings, or the inbox.
export function beforeSettings(): string {
  try {
    const href = sessionStorage.getItem(key);
    if (href && href.startsWith('/') && !href.startsWith('//') && !href.startsWith('/settings')) return href;
  } catch { /* no storage */ }
  return '/';
}

// A field takes Esc for itself (a person typing must never lose the text), and so does anything open over the page.
const typing = 'textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"], [role="combobox"], '
  + 'input:not([type="checkbox"], [type="radio"], [type="button"], [type="submit"], [type="range"], [type="color"], [type="file"])';
const openOverPage = 'dialog[open], [role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"], [aria-modal="true"], .account-popover';

function leavesSettings(event: KeyboardEvent): boolean {
  if (event.key !== 'Escape' || event.defaultPrevented || event.isComposing || event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) return false;
  const target = event.target instanceof Element ? event.target : null;
  return !target?.closest(typing) && !document.querySelector(openOverPage);
}

interface BackToAppProps { href: string; onBack: () => void }

// BackToApp heads the sidebar on every Settings page. Esc does what it does, except where it belongs to something else.
export function BackToApp({ href, onBack }: BackToAppProps) {
  useEffect(() => {
    const leave = (event: KeyboardEvent) => { if (leavesSettings(event)) onBack(); };
    document.addEventListener('keydown', leave);
    return () => document.removeEventListener('keydown', leave);
  }, [onBack]);
  const open = (event: MouseEvent<HTMLAnchorElement>) => {
    // A new tab or window keeps the link's own address.
    if (event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onBack();
  };
  return <a className="settings-exit" href={href} aria-keyshortcuts="Escape" onClick={open}>
    <ArrowLeftIcon /><span>{messages.backToApp}</span><kbd aria-hidden="true">Esc</kbd></a>;
}
