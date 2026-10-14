import type { ReactNode } from 'react';
import type { Me } from '../../api/schemas';
import { messages } from '../../messages';

interface AppShellProps {
  me?: Me;
  signedOut?: boolean;
  brand?: ReactNode;
  links?: ReactNode;
  children: ReactNode;
}

// AppShell names the workspace and person a browser is signed in as, so a second link can never
// silently switch workspaces, and replaces the app with a way back in when there is no session.
export function AppShell({ me, signedOut = false, brand, links, children }: AppShellProps) {
  return <>
    <nav className="app-nav" aria-label={messages.mainNavigation}>
      {brand ?? <span className="wordmark">{messages.brand}</span>}
      {me && <span className="nav-identity"><strong>{me.workspace.name}</strong><span>{me.agent.email}</span></span>}
      <div>{links}</div>
    </nav>
    {signedOut
      ? <main className="redeem-page"><section className="redeem-panel">
          <p role="alert" className="error-copy">{messages.signedOut}</p>
          <p className="muted-copy">{messages.signedOutSelfHosted}</p>
        </section></main>
      : children}
  </>;
}
