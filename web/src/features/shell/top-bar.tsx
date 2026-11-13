import type { ReactNode, Ref } from 'react';
import type { Me } from '../../api/schemas';
import { messages } from '../../messages';
import { SearchIcon } from '../../ui/icons';
import { frameMessages } from './frame-messages';

export interface PageTitle { title: string; icon: ReactNode }

// What the search field needs: the workspace it searches, what opens the palette, and the chunk to start loading
// when a pointer or the focus comes near it.
export interface TopBarSearch { workspace: string; onOpen: () => void; onNear: () => void; field: Ref<HTMLButtonElement> }

// A Mac names the shortcut ⌘K; every other system Ctrl K.
const onMac = () => /Mac|iPhone|iPad/.test(globalThis.navigator?.platform ?? '');

// TopBar heads the sheet of every page: the page's icon and name, the search field in the middle (it opens the
// palette, as Ctrl K or ⌘K does), and Help (the docs, in a new tab). Where the sidebar does not show the workspace
// and the person (folded into its rail, a tablet, a phone), the top bar shows them, so a second link can never
// silently switch workspaces. On a phone the field is a search icon.
export function TopBar({ page, identity, search }: { page?: PageTitle; identity?: Me; search?: TopBarSearch }) {
  return <header className="app-topbar">
    <p className="topbar-title">{page && <>{page.icon}<span>{page.title}</span></>}</p>
    {search && <button type="button" ref={search.field} className="topbar-search" aria-haspopup="dialog" aria-keyshortcuts="Control+K Meta+K"
      onClick={search.onOpen} onPointerEnter={search.onNear} onFocus={search.onNear}>
      <SearchIcon /><span className="topbar-search-label">{frameMessages.searchWorkspace(search.workspace)}</span>
      <kbd aria-hidden="true">{onMac() ? '⌘K' : 'Ctrl K'}</kbd></button>}
    <div className="topbar-end">
      {identity && <section className="identity-bar" aria-label={messages.signedInAs}>
        <strong>{identity.workspace.name}</strong><span>{identity.agent.email}</span>
      </section>}
      <a className="topbar-help" href={messages.docsURL} target="_blank" rel="noopener noreferrer">{messages.help}</a>
    </div>
  </header>;
}
