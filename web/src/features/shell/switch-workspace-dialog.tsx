import { useCallback, useEffect, useId, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import type { Me, WorkspaceChoice } from '../../api/schemas';
import { dialogKeys } from '../../ui/dialog-keys';
import { lazyPart } from '../../ui/lazy-part';
import { WorkspaceRow, WorkspaceRowSkeleton } from '../../ui/workspace-row';
import type { WorkspacesResult } from './other-workspaces';
import { switchMessages as copy } from './switch-workspace-messages';
import './switch-workspace.css';

// The identity provider's client is a chunk of its own, loaded when the dialog opens, never on a page load.
const loader = lazyPart(() => import('./other-workspaces'));

// WorkspaceSwitch is where the menu can offer the person's other workspaces: each desk on its own host,
// with the identity provider (publishableKey) proving who the person is, and APP_ORIGIN signing them in.
// signup is true where APP_ORIGIN lets a person create a workspace.
export interface WorkspaceSwitch {
  publishableKey: string;
  appOrigin: string;
  signup?: boolean;
}

const rowCopy = { current: copy.current, opening: copy.opening };
const byName = (a: WorkspaceChoice, b: WorkspaceChoice) => a.name.localeCompare(b.name) || a.slug.localeCompare(b.slug);

function hostOf(origin: string) {
  try { return new URL(origin).host; } catch { return origin; }
}

// SwitchWorkspaceDialog lists the person's workspaces when it opens: the current one at once, marked and not
// offered, then the others by name, each a link to APP_ORIGIN's sign-in for it, which hands the browser to that
// desk signed in. While the list loads, when it cannot be read and when there is no other workspace, the dialog
// says which: a failure never looks like having no other workspace.
export function SwitchWorkspaceDialog({ me, workspaceSwitch, onClose }: { me: Me; workspaceSwitch: WorkspaceSwitch; onClose: () => void }) {
  const root = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<WorkspacesResult | undefined>(undefined);
  const chunk = loader.use();
  const state = chunk.status === 'error' ? { status: 'failed', reason: 'chunk-failed' } as const : result;
  useEffect(() => { root.current?.focus(); }, []);
  const retry = useCallback(() => { setResult(undefined); setAttempt((count) => count + 1); }, []);
  const appHost = hostOf(workspaceSwitch.appOrigin);
  const listed = state?.status === 'loaded' ? state.workspaces : [];
  const mine = listed.find((workspace) => workspace.slug === me.workspace.slug);
  const current: WorkspaceChoice = mine ?? { slug: me.workspace.slug, name: me.workspace.name, host: window.location.host, roles: me.agent.roles.map((role) => role.name) };
  const others = listed.filter((workspace) => workspace.slug !== me.workspace.slug).sort(byName);
  const keys = dialogKeys(root, onClose);
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') { keys(event); return; }
    const rows = Array.from(root.current?.querySelectorAll<HTMLElement>('a.workspace-row') ?? []);
    if (rows.length === 0) return;
    event.preventDefault();
    const at = rows.indexOf(document.activeElement as HTMLElement);
    const next = event.key === 'ArrowDown' ? (at < 0 ? 0 : Math.min(at + 1, rows.length - 1)) : (at < 0 ? rows.length - 1 : Math.max(at - 1, 0));
    rows[next].focus();
  };
  const hints = state?.status === 'failed' || others.length > 0;
  return (
    <div className="dialog-backdrop switch-workspace-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <div className="switch-workspace" role="dialog" aria-modal="true" aria-labelledby={titleId} ref={root} tabIndex={-1} onKeyDown={onKeyDown}>
        <header className="switch-workspace-head">
          <div><h2 id={titleId}>{copy.title}</h2><p>{copy.signedInAs(me.agent.email)}</p></div>
          <button type="button" className="icon-button" aria-label={copy.close} onClick={onClose}><CloseIcon /></button>
        </header>
        <div className="switch-workspace-list">
          <WorkspaceRow workspace={current} copy={rowCopy} current />
          {!state && <div role="status" aria-label={copy.loading}><WorkspaceRowSkeleton /><WorkspaceRowSkeleton /></div>}
          {others.map((workspace) => <WorkspaceRow key={workspace.slug} workspace={workspace} copy={rowCopy}
            href={`${workspaceSwitch.appOrigin}/sign-in?${new URLSearchParams({ workspace: workspace.slug }).toString()}`} />)}
          {state?.status === 'loaded' && others.length === 0 && <div className="switch-workspace-note">
            <strong>{copy.onlyOneTitle}</strong><p>{copy.onlyOneText}</p>
          </div>}
          {state?.status === 'failed' && <div className="switch-workspace-failed" role="alert">
            <span className="switch-workspace-failed-icon" aria-hidden="true"><AlertIcon /></span>
            <div><strong>{copy.failedTitle}</strong><p>{copy.failedText(appHost)}</p>
              <div className="switch-workspace-actions">
                {state.reason !== 'chunk-failed' && <button type="button" onClick={retry}>{copy.tryAgain}</button>}
                <a href={`${workspaceSwitch.appOrigin}/sign-in`}>{copy.chooseOn(appHost)}</a>
              </div></div>
          </div>}
        </div>
        <footer className="switch-workspace-foot">
          {workspaceSwitch.signup && <a className="switch-workspace-create" href={`${workspaceSwitch.appOrigin}/sign-in?intent=trial`}><PlusIcon />{copy.createWorkspace}</a>}
          {hints && <span className="switch-workspace-keys" aria-hidden="true"><kbd>↑↓</kbd>{copy.keyMove}<kbd>↵</kbd>{copy.keyOpen}<kbd>Esc</kbd>{copy.keyClose}</span>}
        </footer>
        {chunk.status === 'ready' && !result && <chunk.module.OtherWorkspaces key={attempt} publishableKey={workspaceSwitch.publishableKey} onResult={setResult} />}
      </div>
    </div>
  );
}

function CloseIcon() {
  return <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" /></svg>;
}

function PlusIcon() {
  return <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" /></svg>;
}

function AlertIcon() {
  return <svg width="16" height="16" viewBox="0 0 24 24"><path d="M12 8v5M12 16.5v.5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    <circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" strokeWidth="1.75" /></svg>;
}
