import type { WorkspaceChoice } from '../../api/schemas';
import { WorkspaceRow } from '../../ui/workspace-row';
import { switchMessages } from '../shell/switch-workspace-messages';

const rowCopy = { current: switchMessages.current, opening: switchMessages.opening };
const byName = (a: WorkspaceChoice, b: WorkspaceChoice) => a.name.localeCompare(b.name) || a.slug.localeCompare(b.slug);

// WorkspaceChooser lists the verified email's workspaces by name on the sign-in page, in a chunk of its own: it
// only shows after the identity provider has answered, by when the chunk is there. opening marks the workspace
// being opened and holds the others.
export function WorkspaceChooser({ workspaces, opening, pending = false, onSelect }: {
  workspaces: WorkspaceChoice[]; opening?: string; pending?: boolean; onSelect?: (slug: string) => void;
}) {
  return <div className="workspace-choices">
    {[...workspaces].sort(byName).map((workspace) => <WorkspaceRow key={workspace.slug} workspace={workspace} copy={rowCopy}
      opening={opening === workspace.slug} disabled={pending || opening !== undefined} onOpen={() => onSelect?.(workspace.slug)} />)}
  </div>;
}
