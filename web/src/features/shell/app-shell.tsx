import { useEffect } from 'react';
import type { ReactNode } from 'react';
import type { Me } from '../../api/schemas';
import { messages } from '../../messages';
import { AccountMenu } from './account-menu';

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

// AppShell is the icon rail plus the active view. A bar that is always visible names the workspace
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

export function AppShell({ me, appVersion, signedOut = false, brand, links, notice, lockedScreen, onSignOut, signOutFailed, signingOut, children }: AppShellProps) {
  useDismissibleTooltips();
  const view = signedOut
    ? <main className="redeem-page"><section className="redeem-panel">
        <p role="alert" className="error-copy">{messages.signedOut}</p>
        <p className="muted-copy">{messages.signedOutSelfHosted}</p>
      </section></main>
    : lockedScreen ?? children;
  return (
    <div className="app-shell">
      <nav className="app-rail" aria-label={messages.mainNavigation}>
        {brand}
        {/* Signed out, every destination ends on the same screen, so the rail offers none. */}
        {!signedOut && <div className="rail-links">{links}</div>}
        {me && !signedOut && <AccountMenu me={me} appVersion={appVersion} onSignOut={onSignOut} signOutFailed={signOutFailed} signingOut={signingOut} />}
      </nav>
      <div className="app-main">
        {/* Signed out, the old session's identity would be a false claim, so the bar goes with it. */}
        {me && !signedOut && <section className="identity-bar" aria-label={messages.signedInAs}>
          <strong>{me.workspace.name}</strong><span>{me.agent.email}</span>
        </section>}
        {me && !signedOut && !lockedScreen && notice}
        <div className="app-content">
          {view}
        </div>
      </div>
    </div>
  );
}
