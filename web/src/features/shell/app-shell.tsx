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

// AppShell is the icon rail plus the active view. The rail names the workspace and person a browser
// is signed in as, so a second link can never silently switch workspaces, and the view is replaced
// by a way back in when there is no session.
export function AppShell({ me, signedOut = false, brand, links, signIn, children }: AppShellProps) {
  return (
    <div className="app-shell">
      <nav className="app-rail" aria-label={messages.mainNavigation}>
        {brand}
        <div className="rail-links">{links}</div>
        {me && <span className="rail-identity" tabIndex={0}>
          <Avatar name={me.agent.display_name || me.agent.email} />
          <span className="rail-tip"><strong>{me.workspace.name}</strong><span>{me.agent.email}</span></span>
        </span>}
      </nav>
      <div className="app-main">
        {signedOut
          ? <main className="redeem-page"><section className="redeem-panel">
              <p role="alert" className="error-copy">{messages.signedOut}</p>
              {signIn ?? <p className="muted-copy">{messages.signedOutSelfHosted}</p>}
            </section></main>
          : children}
      </div>
    </div>
  );
}
