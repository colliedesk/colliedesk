import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import type { Me } from '../../api/schemas';
import { messages } from '../../messages';
import { Avatar } from '../../ui/avatar';
import { lazyPart, Part, PartUnavailable } from '../../ui/lazy-part';
import { switchMessages } from './switch-workspace-messages';

// The dialog is a chunk of its own: nobody pays for it until they report a problem.
const ReportProblemDialog = lazy(() => import('./report-problem-dialog').then((module) => ({ default: module.ReportProblemDialog })));
// So is Switch workspace's, fetched from this server as the menu opens, so it is there by the time it is chosen.
const switchPart = lazyPart(() => import('./switch-workspace-dialog'));

// WorkspaceSwitch is where the menu can offer the person's other workspaces: each desk on its own host,
// with the identity provider (publishableKey) proving who the person is, and APP_ORIGIN signing them in.
// signup is true where APP_ORIGIN lets a person create a workspace.
export interface WorkspaceSwitch {
  publishableKey: string;
  appOrigin: string;
  signup?: boolean;
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
// workspace and the address are in the top bar, so the menu does not repeat them. With workspaceSwitch it offers
// Switch workspace… at once, whatever the person's workspaces are: the dialog reads them when it opens and says
// when it cannot.
export function AccountMenu({ me, appVersion, onSignOut, signOutFailed = false, signingOut = false, workspaceSwitch }: AccountMenuProps) {
  const [open, setOpen] = useState(false);
  const [reporting, setReporting] = useState(false);
  const [switching, setSwitching] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const name = me.agent.display_name || me.agent.email;
  const roles = me.agent.roles.map((role) => role.name).join(', ');
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
      <button type="button" ref={button} className="rail-avatar" aria-expanded={open} onClick={() => { if (workspaceSwitch) switchPart.preload(); setOpen(!open); }}>
        <Avatar name={name} size="sm" /><span className="sr-only">{messages.account}, </span>
        <span className="current-user"><span className="current-user-name">{name}</span>
          {me.agent.display_name && <> <span className="current-user-email">{me.agent.email}</span></>}</span>
      </button>
      {open && <div className="account-popover" role="group" aria-label={messages.account}>
        <p><strong>{name}</strong><span>{[roles, me.agent.email].filter(Boolean).join(' · ')}</span></p>
        {signOutFailed && <p role="alert" className="error-copy">{messages.signOutFailed}</p>}
        {workspaceSwitch && <button type="button" className="account-item" onClick={() => { setOpen(false); setSwitching(true); }}>
          <SwitchIcon />{switchMessages.menuItem}</button>}
        <button type="button" className="account-item" onClick={() => { setOpen(false); setReporting(true); }}><FlagIcon />{messages.reportProblem}</button>
        <hr />
        <button type="button" className="account-item" disabled={signingOut} onClick={onSignOut}><SignOutIcon />{messages.signOut}</button>
      </div>}
      {switching && workspaceSwitch && <Part part={switchPart} failed={<PartUnavailable />} render={({ SwitchWorkspaceDialog }) =>
        <SwitchWorkspaceDialog me={me} workspaceSwitch={workspaceSwitch} onClose={() => { setSwitching(false); button.current?.focus(); }} />} />}
      {reporting && <Suspense fallback={null}>
        <ReportProblemDialog appVersion={appVersion} onClose={() => { setReporting(false); button.current?.focus(); }} />
      </Suspense>}
    </div>
  );
}

const icon = { width: 16, height: 16, viewBox: '0 0 24 24', 'aria-hidden': true } as const;
const stroke = { fill: 'none', stroke: 'currentColor', strokeWidth: 1.75, strokeLinecap: 'round', strokeLinejoin: 'round' } as const;

function SwitchIcon() {
  return <svg {...icon}><path d="M7 4 3 8l4 4M3 8h14M17 20l4-4-4-4M21 16H7" {...stroke} /></svg>;
}

function FlagIcon() {
  return <svg {...icon}><path d="M4 21V4h12l-2 4 2 4H4" {...stroke} /></svg>;
}

function SignOutIcon() {
  return <svg {...icon}><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" {...stroke} /></svg>;
}
