import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import type { CSSProperties, ReactNode } from 'react';
import type { Me } from '../../api/schemas';
import { messages } from '../../messages';
import { PanelRightIcon, PenIcon } from '../../ui/icons';
import { lazyPart, Part } from '../../ui/lazy-part';
import { AccountMenu } from './account-menu';
import type { WorkspaceSwitch } from './account-menu';
import type { PaletteSource } from './palette-entries';
import { useSidebarLayout } from './sidebar-layout';
import type { SidebarChannel, SidebarView } from './sidebar-items';
import { InviteLink, SidebarLists } from './sidebar-parts';
import { SidebarResizer } from './sidebar-resizer';
import { TopBar } from './top-bar';
import type { PageTitle } from './top-bar';

// The palette is a chunk of its own: it starts loading when a pointer or the focus reaches the search field.
const palettePart = lazyPart(() => import('./command-palette'));

interface AppShellProps {
  me?: Me;
  signedOut?: boolean;
  // appVersion is the server's version, sent with a problem report.
  appVersion?: string;
  brand?: ReactNode;
  // links are the sidebar's destinations (Inbox, Knowledge base, Reports); footLinks sit at its foot (Settings).
  links?: ReactNode;
  footLinks?: ReactNode;
  // page is the icon and name the top bar shows for the page on screen.
  page?: PageTitle;
  // palette is what the top bar's search offers (its field and Ctrl K or ⌘K open it); none, no search.
  palette?: PaletteSource;
  // quickStart sits in the sidebar head beside the workspace; onNewConversation is there for someone who may start one.
  quickStart?: ReactNode;
  onNewConversation?: () => void;
  views?: SidebarView[];
  channels?: SidebarChannel[];
  canInvite?: boolean;
  // notice sits above the sheet on every view, for example the trial banner.
  notice?: ReactNode;
  // lockedScreen replaces the view when the workspace is locked; the workspace and the person stay on screen.
  lockedScreen?: ReactNode;
  onSignOut?: () => void;
  signOutFailed?: boolean;
  signingOut?: boolean;
  // workspaceSwitch lets the account menu offer the person's other workspaces.
  workspaceSwitch?: WorkspaceSwitch;
  children: ReactNode;
}

// AppShell is the sidebar plus the active view on a white sheet under its top bar. The workspace and the person a
// browser is signed in as are always on screen, so a second link can never silently switch workspaces: the open
// sidebar names both (its head and its foot), and where it does not (folded, a tablet, a phone) the top bar does.
// The view is replaced by a way back in when there is no session.
// Tooltips can be dismissed with Escape (WCAG 1.4.13) and return on the next pointer move or focus change.
export function useDismissibleTooltips() {
  useEffect(() => {
    const root = document.documentElement;
    const dismiss = (event: KeyboardEvent) => { if (event.key === 'Escape') root.setAttribute('data-tips-off', ''); };
    const restore = () => root.removeAttribute('data-tips-off');
    document.addEventListener('keydown', dismiss);
    document.addEventListener('pointermove', restore);
    document.addEventListener('focusin', restore);
    return () => {
      document.removeEventListener('keydown', dismiss);
      document.removeEventListener('pointermove', restore);
      document.removeEventListener('focusin', restore);
      restore();
    };
  }, []);
}

// The width from which the sidebar shows names (layout.css): narrower, it is a rail or a phone's top bar.
const namesQuery = '(min-width: 64.01rem)';
const showsNames = () => window.matchMedia?.(namesQuery).matches ?? true;
const subscribeNames = (change: () => void) => {
  const query = window.matchMedia?.(namesQuery);
  query?.addEventListener('change', change);
  return () => query?.removeEventListener('change', change);
};

// usePalette opens the search palette from the field or from Ctrl K / ⌘K anywhere, centred on the field, and gives the
// focus back to where it was when the palette closes without a choice.
function usePalette(enabled: boolean) {
  const field = useRef<HTMLButtonElement>(null);
  const returnTo = useRef<HTMLElement | null>(null);
  const [anchor, setAnchor] = useState<{ center: number; top: number } | null>(null);
  const isOpen = useRef(false);
  const open = useCallback(() => {
    palettePart.preload();
    // Pressed again while open, the shortcut leaves the palette as it is.
    if (isOpen.current) return;
    isOpen.current = true;
    const focused = document.activeElement;
    returnTo.current = focused instanceof HTMLElement && focused !== document.body ? focused : field.current;
    const box = field.current?.getBoundingClientRect();
    // A field on screen anchors it; on a phone (an icon) or without one, the palette takes the middle.
    setAnchor(box && box.width > 64 ? { center: box.left + box.width / 2, top: Math.max(box.top - 4, 8) } : { center: window.innerWidth / 2, top: 8 });
  }, []);
  const close = useCallback((restoreFocus: boolean) => {
    isOpen.current = false;
    setAnchor(null);
    const target = returnTo.current;
    returnTo.current = null;
    if (restoreFocus && target?.isConnected) target.focus();
  }, []);
  useEffect(() => {
    if (!enabled) return;
    const shortcut = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== 'k' || !(event.ctrlKey || event.metaKey) || event.altKey || event.shiftKey) return;
      event.preventDefault();
      open();
    };
    document.addEventListener('keydown', shortcut);
    return () => document.removeEventListener('keydown', shortcut);
  }, [enabled, open]);
  return { field, anchor, open, close };
}

export function AppShell({ me, appVersion, signedOut = false, brand, links, footLinks, page, palette, quickStart, onNewConversation, views, channels, canInvite = false,
  notice, lockedScreen, onSignOut, signOutFailed, signingOut, workspaceSwitch, children }: AppShellProps) {
  useDismissibleTooltips();
  const sidebar = useSidebarLayout();
  const { collapsed } = sidebar.layout;
  const wide = useSyncExternalStore(subscribeNames, showsNames);
  const toggleLabel = collapsed ? messages.expandSidebar : messages.collapseSidebar;
  // Signed out, the old session's identity would be a false claim, so neither the sidebar nor the top bar names it.
  const session = me && !signedOut ? me : undefined;
  // The open sidebar shows the workspace and the person; it is then the region that names them.
  const sidebarNames = Boolean(session) && wide && !collapsed;
  const search = usePalette(Boolean(session && palette));
  const view = signedOut
    ? <main className="redeem-page"><section className="redeem-panel">
        <p role="alert" className="error-copy">{messages.signedOut}</p>
        <p className="muted-copy">{messages.signedOutSelfHosted}</p>
      </section></main>
    : lockedScreen ?? children;
  return (
    <div className="app-shell" data-sidebar={collapsed ? 'collapsed' : 'expanded'}
      style={{ '--sidebar-user-width': `${sidebar.layout.width}px` } as CSSProperties}>
      {/* On a desktop the sidebar can be resized from its edge and folded into an icon rail; both are kept per browser. */}
      <div className="app-sidebar" {...(sidebarNames ? { role: 'region', 'aria-label': messages.signedInAs } : {})}>
        <nav className="app-rail" aria-label={messages.mainNavigation}>
          <div className="rail-head">
            {brand}
            {session && <strong className="workspace-name">{session.workspace.name}</strong>}
            {session && quickStart}
            {session && onNewConversation && <button type="button" className="sidebar-new" aria-label={messages.newConversation} data-tip={messages.newConversation}
              data-opens="new-conversation" onClick={onNewConversation}><PenIcon /></button>}
            <button type="button" className="sidebar-toggle" aria-label={toggleLabel} data-tip={toggleLabel} aria-expanded={!collapsed}
              onClick={() => sidebar.set({ ...sidebar.layout, collapsed: !collapsed })}><PanelRightIcon /></button>
          </div>
          {/* Signed out, every destination ends on the same screen, so the rail offers none. */}
          {/* The places and the lists scroll between the head and the foot, so the person at the foot stays on screen. */}
          <div className="sidebar-scroll">
            {!signedOut && <div className="rail-links">{links}</div>}
            {session && <SidebarLists views={views} channels={channels} />}
          </div>
          {session && <div className="sidebar-foot">
            {canInvite && <InviteLink />}
            {footLinks}
            <AccountMenu me={session} appVersion={appVersion} onSignOut={onSignOut} signOutFailed={signOutFailed} signingOut={signingOut}
              workspaceSwitch={workspaceSwitch} />
          </div>}
          {!session && !signedOut && footLinks}
        </nav>
        <SidebarResizer {...sidebar} />
      </div>
      <div className="app-main">
        {session && !lockedScreen && notice}
        {/* The white sheet the view sits on, under its top bar. */}
        <div className="app-sheet">
          {session && <TopBar page={lockedScreen ? undefined : page} identity={sidebarNames ? undefined : session}
            search={palette ? { workspace: session.workspace.name, onOpen: search.open, onNear: palettePart.preload, field: search.field } : undefined} />}
          <div className="app-content">
            {view}
          </div>
        </div>
      </div>
      {session && palette && search.anchor && <Part part={palettePart} render={({ CommandPalette }) =>
        <CommandPalette {...palette} workspaceId={session.workspace.id} anchor={search.anchor ?? undefined} onClose={search.close} />} />}
    </div>
  );
}
