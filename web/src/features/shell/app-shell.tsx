import { useEffect, useRef, useState } from 'react';
import type { CSSProperties, KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent, ReactNode } from 'react';
import type { Me } from '../../api/schemas';
import { messages } from '../../messages';
import { PanelRightIcon } from '../../ui/icons';
import { AccountMenu } from './account-menu';
import { clampWidth, foldBelow, sidebarWidth, useSidebarLayout } from './sidebar-layout';
import type { SidebarLayout } from './sidebar-layout';

interface AppShellProps {
  me?: Me;
  signedOut?: boolean;
  // appVersion is the server's version, sent with a problem report.
  appVersion?: string;
  brand?: ReactNode;
  links?: ReactNode;
  // notice sits under the identity bar on every view, for example the trial banner.
  notice?: ReactNode;
  // lockedScreen replaces the view when the workspace is locked; the identity bar stays.
  lockedScreen?: ReactNode;
  onSignOut?: () => void;
  signOutFailed?: boolean;
  signingOut?: boolean;
  children: ReactNode;
}

// AppShell is the sidebar plus the active view on a white sheet. A bar that is always visible names the workspace
// and person a browser is signed in as, so a second link can never silently switch workspaces, and the view is replaced
// by a way back in when there is no session.
// Tooltips can be dismissed with Escape (WCAG 1.4.13) and return on the next pointer move or focus change.
function useDismissibleTooltips() {
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

// The folded rail's width; layout.css gives the same number to --sidebar-width.
const railWidth = 56;
const keyStep = 16;

// SidebarResizer is the sidebar's right edge on a desktop: drag it to resize, drag it far left to fold the sidebar
// into its rail, double-click to restore the default width. As a separator it also takes the arrow keys, Home and End,
// and Enter folds or opens it.
function SidebarResizer({ layout, set, preview }: ReturnType<typeof useSidebarLayout>) {
  const drag = useRef<{ x: number; from: number; width: number; last: SidebarLayout } | null>(null);
  const [dragging, setDragging] = useState(false);
  useEffect(() => {
    if (!dragging) return;
    document.documentElement.setAttribute('data-sidebar-resizing', '');
    return () => document.documentElement.removeAttribute('data-sidebar-resizing');
  }, [dragging]);
  const at = (clientX: number): SidebarLayout => {
    const start = drag.current;
    if (!start) return layout;
    const raw = start.from + clientX - start.x;
    return raw < foldBelow ? { width: start.width, collapsed: true } : { width: clampWidth(raw), collapsed: false };
  };
  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = { x: event.clientX, from: layout.collapsed ? railWidth : layout.width, width: layout.width, last: layout };
    setDragging(true);
  };
  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!drag.current) return;
    drag.current.last = at(event.clientX);
    preview(drag.current.last);
  };
  const end = () => {
    if (!drag.current) return;
    set(drag.current.last);
    drag.current = null;
    setDragging(false);
  };
  const onKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    const next: Record<string, SidebarLayout | undefined> = {
      ArrowLeft: layout.collapsed ? undefined : layout.width - keyStep < sidebarWidth.min ? { ...layout, collapsed: true } : { ...layout, width: layout.width - keyStep },
      ArrowRight: layout.collapsed ? { ...layout, collapsed: false } : { ...layout, width: layout.width + keyStep },
      Home: { ...layout, collapsed: true },
      End: { width: sidebarWidth.max, collapsed: false },
      Enter: { ...layout, collapsed: !layout.collapsed },
    };
    const value = next[event.key];
    if (!value) return;
    event.preventDefault();
    set(value);
  };
  return <div className="sidebar-resizer" role="separator" aria-orientation="vertical" aria-label={messages.resizeSidebar} tabIndex={0}
    aria-valuemin={railWidth} aria-valuemax={sidebarWidth.max} aria-valuenow={layout.collapsed ? railWidth : layout.width}
    data-dragging={dragging || undefined}
    onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={end} onPointerCancel={end} onLostPointerCapture={end}
    onDoubleClick={() => set({ width: sidebarWidth.initial, collapsed: false })} onKeyDown={onKeyDown} />;
}

export function AppShell({ me, appVersion, signedOut = false, brand, links, notice, lockedScreen, onSignOut, signOutFailed, signingOut, children }: AppShellProps) {
  useDismissibleTooltips();
  const sidebar = useSidebarLayout();
  const { collapsed } = sidebar.layout;
  const toggleLabel = collapsed ? messages.expandSidebar : messages.collapseSidebar;
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
      <div className="app-sidebar">
        <nav className="app-rail" aria-label={messages.mainNavigation}>
          <div className="rail-head">
            {brand}
            <button type="button" className="sidebar-toggle" aria-label={toggleLabel} data-tip={toggleLabel} aria-expanded={!collapsed}
              onClick={() => sidebar.set({ ...sidebar.layout, collapsed: !collapsed })}><PanelRightIcon /></button>
          </div>
          {/* Signed out, every destination ends on the same screen, so the rail offers none. */}
          {!signedOut && <div className="rail-links">{links}</div>}
          {me && !signedOut && <AccountMenu me={me} appVersion={appVersion} onSignOut={onSignOut} signOutFailed={signOutFailed} signingOut={signingOut} />}
        </nav>
        <SidebarResizer {...sidebar} />
      </div>
      <div className="app-main">
        {/* Signed out, the old session's identity would be a false claim, so the bar goes with it. */}
        {me && !signedOut && <section className="identity-bar" aria-label={messages.signedInAs}>
          <strong>{me.workspace.name}</strong><span>{me.agent.email}</span>
        </section>}
        {me && !signedOut && !lockedScreen && notice}
        {/* The white sheet the view sits on; a wrapper for its look only. */}
        <div className="app-sheet">
          <div className="app-content">
            {view}
          </div>
        </div>
      </div>
    </div>
  );
}
