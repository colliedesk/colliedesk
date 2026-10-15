import { useEffect } from 'react';
import type { ReactNode } from 'react';
import type { Me } from '../../api/schemas';
import { messages } from '../../messages';
import { Avatar } from '../../ui/avatar';

interface AppShellProps {
  me?: Me;
  signedOut?: boolean;
  brand?: ReactNode;
  links?: ReactNode;
  // signIn is shown on the signed-out screen of instances with cloud sign-in.
  signIn?: ReactNode;
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

export function AppShell({ me, signedOut = false, brand, links, signIn, children }: AppShellProps) {
  useDismissibleTooltips();
  return (
    <div className="app-shell">
      <nav className="app-rail" aria-label={messages.mainNavigation}>
        {brand}
        <div className="rail-links">{links}</div>
        {me && <span className="rail-avatar"><Avatar name={me.agent.display_name || me.agent.email} /></span>}
      </nav>
      <div className="app-main">
        {me && <section className="identity-bar" aria-label={messages.signedInAs}>
          <strong>{me.workspace.name}</strong><span>{me.agent.email}</span>
        </section>}
        <div className="app-content">
          {signedOut
            ? <main className="redeem-page"><section className="redeem-panel">
                <p role="alert" className="error-copy">{messages.signedOut}</p>
                {signIn ?? <p className="muted-copy">{messages.signedOutSelfHosted}</p>}
              </section></main>
            : children}
        </div>
      </div>
    </div>
  );
}
