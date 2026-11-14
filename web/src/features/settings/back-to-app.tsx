import { useEffect } from 'react';
import type { MouseEvent } from 'react';
import { messages } from '../../messages';
import { ArrowLeftIcon } from '../../ui/icons';

// Settings takes over the sidebar, so the page a person was on stays one press away: Back to app, and Esc. The page
// is kept for the tab (also across a reload of Settings), with the workspace and the person it was kept for, and
// only ever an address inside the app. A tab can change hands (a sign-out, another workspace), and the next
// person must never be offered the last one's page.
const key = 'colliedesk:before-settings';

export function rememberBeforeSettings(href: string, owner: string): void {
  try { sessionStorage.setItem(key, JSON.stringify({ href, owner })); } catch { /* storage refused: Back to app opens the inbox */ }
}

// forgetBeforeSettings runs where a sign-out succeeds.
export function forgetBeforeSettings(): void {
  try { sessionStorage.removeItem(key); } catch { /* no storage */ }
}

// beforeSettings is where Back to app goes for this person (owner): the page they left for Settings, or the inbox.
export function beforeSettings(owner: string): string {
  try {
    const kept = JSON.parse(sessionStorage.getItem(key) ?? 'null') as { href?: unknown; owner?: unknown } | null;
    const href = kept?.href;
    if (kept?.owner === owner && typeof href === 'string' && href.startsWith('/') && !href.startsWith('//') && !href.startsWith('/settings')) return href;
  } catch { /* no storage, or a value that is not ours */ }
  return '/';
}

// Esc leaves Settings only from where nothing of the page can own it: the page itself (focus on the body) or the
// sidebar's own menu and Back to app. Focus anywhere in the page's content (a field, a checkbox, a panel with its
// own Esc) keeps the key for that, so typed text and open pickers are never dropped.
const fromTheFrame = '.settings-nav, .settings-exit';
// Whatever is open over the page owns Esc, wherever the focus is. The listener runs at capture, before any of
// these has closed itself, so they are still here to be seen.
const openOverPage = 'dialog[open], [role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"], [aria-modal="true"], .account-popover, .confirm-panel, .invite-roles';

function leavesSettings(event: KeyboardEvent): boolean {
  if (event.key !== 'Escape' || event.defaultPrevented || event.isComposing || event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) return false;
  const target = event.target instanceof Element ? event.target : null;
  const onPage = !target || target === document.body || target === document.documentElement;
  if (!onPage && !target.closest(fromTheFrame)) return false;
  // A focused control showing its tooltip keeps Esc to dismiss it (use-dismissible-tooltips).
  try { if (target?.closest('[data-tip]')?.matches(':focus-visible')) return false; } catch { /* a browser without :focus-visible */ }
  return !document.querySelector(openOverPage);
}

interface BackToAppProps { href: string; onBack: () => void }

// BackToApp heads the sidebar on every Settings page, and takes the Esc that is for leaving.
export function BackToApp({ href, onBack }: BackToAppProps) {
  useEffect(() => {
    const leave = (event: KeyboardEvent) => { if (leavesSettings(event)) onBack(); };
    document.addEventListener('keydown', leave, true);
    return () => document.removeEventListener('keydown', leave, true);
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
