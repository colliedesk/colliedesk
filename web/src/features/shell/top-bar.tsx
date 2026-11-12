import type { ReactNode } from 'react';
import type { Me } from '../../api/schemas';
import { messages } from '../../messages';

export interface PageTitle { title: string; icon: ReactNode }

// TopBar heads the sheet of every page: the page's icon and name, and Help (the docs, in a new tab). Where the
// sidebar does not show the workspace and the person (folded into its rail, a tablet, a phone), the top bar shows
// them, so a second link can never silently switch workspaces.
export function TopBar({ page, identity }: { page?: PageTitle; identity?: Me }) {
  return <header className="app-topbar">
    {page && <p className="topbar-title">{page.icon}<span>{page.title}</span></p>}
    {identity && <section className="identity-bar" aria-label={messages.signedInAs}>
      <strong>{identity.workspace.name}</strong><span>{identity.agent.email}</span>
    </section>}
    <a className="topbar-help" href={messages.docsURL} target="_blank" rel="noopener noreferrer">{messages.help}</a>
  </header>;
}
