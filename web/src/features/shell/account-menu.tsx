import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import type { Me, WorkspaceChoice } from '../../api/schemas';
import { messages } from '../../messages';
import { Avatar } from '../../ui/avatar';

// The dialog is a chunk of its own: nobody pays for it until they report a problem.
const ReportProblemDialog = lazy(() => import('./report-problem-dialog').then((module) => ({ default: module.ReportProblemDialog })));
// So is the identity provider's client, loaded only where a person may switch workspace.
const OtherWorkspaces = lazy(() => import('./other-workspaces').then((module) => ({ default: module.OtherWorkspaces })));

// WorkspaceSwitch is where the menu can offer the person's other workspaces: each desk on its own host,
// with the identity provider (publishableKey) proving who the person is, and APP_ORIGIN signing them in.
export interface WorkspaceSwitch {
  publishableKey: string;
  appOrigin: string;
}

interface AccountMenuProps {
  me: Me;
  appVersion?: string;
  onSignOut?: () => void;
  signOutFailed?: boolean;
  signingOut?: boolean;
  workspaceSwitch?: WorkspaceSwitch;
}

// AccountMenu is the person at the foot of the sidebar (their name, and their address under it), which opens who is
// signed in, as which role, and the way out. Folded into the rail or on a phone it is the avatar alone, and the
// workspace and the address are in the top bar, so the menu does not repeat them. With workspaceSwitch it also
// lists the person's other workspaces, each a link that signs them in there through APP_ORIGIN; the list is
// asked for as soon as the pointer or the focus reaches the avatar, so it is there when the menu opens.
export function AccountMenu({ me, appVersion, onSignOut, signOutFailed = false, signingOut = false, workspaceSwitch }: AccountMenuProps) {
  const [open, setOpen] = useState(false);
  const [reporting, setReporting] = useState(false);
  const [wanted, setWanted] = useState(false);
  const [workspaces, setWorkspaces] = useState<WorkspaceChoice[]>([]);
  const others = workspaceSwitch ? workspaces.filter((workspace) => workspace.slug !== me.workspace.slug) : [];
  const want = () => setWanted(true);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const name = me.agent.display_name || me.agent.email;
  useEffect(() => {
    if (!open) return;
    const closeOutside = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) setOpen(false); };
    document.addEventListener('pointerdown', closeOutside);
    return () => document.removeEventListener('pointerdown', closeOutside);
  }, [open]);
  const close = () => { setOpen(false); button.current?.focus(); };
  return (
    <div className="account-menu" ref={root} onKeyDown={(event) => { if (event.key === 'Escape' && open) close(); }}
      onBlur={(event) => { if (open && !root.current?.contains(event.relatedTarget as Node | null)) setOpen(false); }}>
      <button type="button" ref={button} className="rail-avatar" aria-expanded={open} onPointerEnter={want} onFocus={want}
        onClick={() => { want(); setOpen(!open); }}>
        <Avatar name={name} size="sm" /><span className="sr-only">{messages.account}, </span>
        <span className="current-user"><span className="current-user-name">{name}</span>
          {me.agent.display_name && <> <span className="current-user-email">{me.agent.email}</span></>}</span>
      </button>
      {open && <div className="account-popover" role="group" aria-label={messages.account}>
        <p><strong>{name}</strong><span>{me.agent.roles.map((role) => role.name).join(", ")}</span></p>
        {others.length > 0 && workspaceSwitch && <div role="group" aria-label={messages.switchWorkspace} className="account-workspaces">
          <span aria-hidden="true">{messages.switchWorkspace}</span>
          {others.map((workspace) => <a key={workspace.slug} className="button"
            href={`${workspaceSwitch.appOrigin}/sign-in?${new URLSearchParams({ workspace: workspace.slug }).toString()}`}>{workspace.name}</a>)}
        </div>}
        {signOutFailed && <p role="alert" className="error-copy">{messages.signOutFailed}</p>}
        <button type="button" onClick={() => { setOpen(false); setReporting(true); }}>{messages.reportProblem}</button>
        <button type="button" disabled={signingOut} onClick={onSignOut}>{messages.signOut}</button>
      </div>}
      {workspaceSwitch && wanted && <Suspense fallback={null}>
        <OtherWorkspaces publishableKey={workspaceSwitch.publishableKey} onLoaded={setWorkspaces} />
      </Suspense>}
      {reporting && <Suspense fallback={null}>
        <ReportProblemDialog appVersion={appVersion} onClose={() => { setReporting(false); button.current?.focus(); }} />
      </Suspense>}
    </div>
  );
}
