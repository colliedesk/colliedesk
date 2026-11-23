import { useId } from 'react';
import type { WorkspaceChoice } from '../api/schemas';
import './workspace-row.css';

export interface WorkspaceRowCopy {
  current: string;
  opening: string;
}

interface WorkspaceRowProps {
  workspace: WorkspaceChoice;
  copy: WorkspaceRowCopy;
  // current marks the workspace on screen: it is shown, not offered, so it is no link and no button.
  current?: boolean;
  // opening marks the row whose sign-in is on its way (the sign-in chooser), with a spinner.
  opening?: boolean;
  disabled?: boolean;
  // href makes the row a link; onOpen a button.
  href?: string;
  onOpen?: () => void;
}

// The tile shows the workspace's first letter: enough to tell two workspaces apart at a glance.
function initial(name: string) {
  return Array.from(name.trim())[0]?.toUpperCase() ?? '?';
}

// WorkspaceRow is one workspace in the Switch workspace dialog and the sign-in chooser: initial tile, name,
// address, the person's roles and a chevron, each in a fixed slot so rows line up. Its name names the link or
// button; the address and the roles describe it.
export function WorkspaceRow({ workspace, copy, current = false, opening = false, disabled = false, href, onOpen }: WorkspaceRowProps) {
  const nameId = useId();
  const detailId = useId();
  const roles = workspace.roles.join(', ');
  const details = [workspace.host, roles].filter(Boolean).join(', ');
  const body = <>
    <span className="workspace-row-tile" aria-hidden="true">{initial(workspace.name)}</span>
    <span className="workspace-row-text">
      <span className="workspace-row-name"><span id={nameId}>{workspace.name}</span>{current && <span className="workspace-row-current">{` · ${copy.current}`}</span>}</span>
      {workspace.host && <span className="workspace-row-host" aria-hidden={!current}>{workspace.host}</span>}
    </span>
    <span className="workspace-row-role" aria-hidden={!current}>{opening ? copy.opening : roles}</span>
    <span className="workspace-row-trailing" aria-hidden="true">{current ? <Check /> : opening ? <Spinner /> : <Chevron />}</span>
    {!current && <span id={detailId} className="sr-only">{details}</span>}
  </>;
  if (current) return <div className="workspace-row" data-current="">{body}</div>;
  const named = { 'aria-labelledby': nameId, 'aria-describedby': detailId };
  if (href) return <a className="workspace-row" href={href} {...named}>{body}</a>;
  return <button type="button" className="workspace-row" onClick={onOpen} disabled={disabled} aria-busy={opening || undefined} {...named}>{body}</button>;
}

// WorkspaceRowSkeleton holds a row's place while the list loads.
export function WorkspaceRowSkeleton() {
  return <div className="workspace-row workspace-row-skeleton" aria-hidden="true">
    <span className="workspace-row-tile" />
    <span className="workspace-row-text"><i /><i /></span>
  </div>;
}

function Chevron() {
  return <svg width="16" height="16" viewBox="0 0 24 24"><path d="m9 6 6 6-6 6" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" /></svg>;
}

function Check() {
  return <svg width="16" height="16" viewBox="0 0 24 24"><path d="m5 12 5 5 9-10" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" /></svg>;
}

function Spinner() {
  return <svg className="workspace-row-spinner" width="16" height="16" viewBox="0 0 24 24">
    <circle cx="12" cy="12" r="8" fill="none" stroke="var(--line)" strokeWidth="2" />
    <path d="M12 4a8 8 0 0 1 8 8" fill="none" stroke="var(--text-2)" strokeWidth="2" strokeLinecap="round" />
  </svg>;
}
