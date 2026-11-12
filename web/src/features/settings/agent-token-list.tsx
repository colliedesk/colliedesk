import { useEffect, useId, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import type { Session } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { agentTokenMessages as text } from './agent-tokens-messages';
import { RevokeTokenDialog } from './revoke-token-dialog';
import { settingsMessages } from './settings-messages';
import { permissionLabel } from './token-permissions';

// A token ends when it is revoked or reaches its end date; until then it can be revoked (also while stopped).
function ended(session: Session): 'revoked' | 'expired' | null {
  if (session.revoked_at) return 'revoked';
  return Date.parse(session.expires_at) <= Date.now() ? 'expired' : null;
}

// TokenAccess says what a token may do: the role it follows, a fixed list, the tier it had before roles, or
// that it is stopped while its person lacks a permission of its role. "What it may do" opens the list.
function TokenAccess({ session }: { session: Session }) {
  const token = session.token!;
  if (token.kind === 'role' && token.role && !token.role_held) {
    return <div className="token-access"><span className="token-stopped-line"><span className="token-dot" aria-hidden="true" />
      {settingsMessages.stoppedToken(session.agent_name, token.role.name)}</span><span className="token-access-help">{settingsMessages.stoppedHelp}</span></div>;
  }
  const badge = token.kind === 'role' && token.role ? settingsMessages.followsRole(token.role.name)
    : token.kind === 'legacy' ? settingsMessages.legacyToken(token.legacy_tier ?? 'custom') : settingsMessages.fixedToken(token.permissions.length);
  return <div className="token-access"><span className="token-badge">{badge}</span>
    <details className="token-may-do"><summary>{settingsMessages.tokenPermissions}</summary>
      <ul>{token.permissions.map((name) => <li key={name}>{permissionLabel(name)}</li>)}</ul></details></div>;
}

// RowMenu is the row's "⋯" button with a menu under it: Escape or a press outside closes it.
function RowMenu({ label, onRevoke }: { label: string; onRevoke: () => void }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return undefined;
    menu.current?.querySelector<HTMLElement>('[role=menuitem]')?.focus();
    const outside = (event: PointerEvent) => {
      if (!menu.current?.contains(event.target as Node) && !button.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', outside);
    return () => document.removeEventListener('pointerdown', outside);
  }, [open]);
  const keys = (event: KeyboardEvent) => {
    if (event.key === 'Escape') { event.preventDefault(); setOpen(false); button.current?.focus(); }
    if (event.key === 'Tab') setOpen(false);
  };
  return <div className="token-menu-wrap">
    <button ref={button} type="button" className="icon-button token-menu-button" aria-label={text.rowActions(label)} aria-haspopup="menu" aria-expanded={open}
      aria-controls={open ? id : undefined} onClick={() => setOpen(!open)}>
      <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true"><circle cx="5" cy="12" r="1.6" /><circle cx="12" cy="12" r="1.6" /><circle cx="19" cy="12" r="1.6" /></svg>
    </button>
    {open && <div ref={menu} id={id} className="token-menu" role="menu" aria-label={text.rowActions(label)} onKeyDown={keys}>
      <button type="button" role="menuitem" className="danger-action" onClick={() => { setOpen(false); onRevoke(); }}>{text.revoke}</button>
    </div>}
  </div>;
}

interface AgentTokenListProps {
  tokens: Session[];
  timezone: string;
  onRevoke: (id: string) => void;
  labelledBy: string;
}

// AgentTokenList lists the agent tokens on External AI agents: who made each one, what it may do, when an agent
// last used it (the session's last_seen_at) and when it ends. Revoke, from the row menu, asks first.
export function AgentTokenList({ tokens, timezone, onRevoke, labelledBy }: AgentTokenListProps) {
  const [confirming, setConfirming] = useState<Session | null>(null);
  if (tokens.length === 0) return <p className="muted-copy token-list-empty">{text.noTokens}</p>;
  return <>
    <table className="token-table" aria-labelledby={labelledBy}>
      <thead><tr><th scope="col">{text.columns.token}</th><th scope="col">{text.columns.access}</th><th scope="col">{text.columns.lastUsed}</th>
        <th scope="col">{text.columns.expires}</th><th scope="col"><span className="sr-only">{text.columns.actions}</span></th></tr></thead>
      <tbody>{tokens.map((session) => {
        const end = ended(session);
        return <tr key={session.id} className={end ? 'token-ended' : undefined}>
          <td className="token-name"><strong>{session.label}</strong><span>{session.agent_email}</span></td>
          <td><TokenAccess session={session} /></td>
          <td className="token-when" data-label={text.columns.lastUsed}><time dateTime={session.last_seen_at}>{formatDate(session.last_seen_at, timezone)}</time></td>
          <td className="token-when token-expires" data-label={text.columns.expires}>{end
            ? <span className="token-end"><span className="token-dot token-dot-hollow" aria-hidden="true" />{end === 'revoked' ? text.revoked : text.expired}</span>
            : <time dateTime={session.expires_at}>{formatDate(session.expires_at, timezone)}</time>}</td>
          <td className="token-actions">{!end && <RowMenu label={session.label} onRevoke={() => setConfirming(session)} />}</td>
        </tr>;
      })}</tbody>
    </table>
    {confirming && <RevokeTokenDialog session={confirming} onClose={() => setConfirming(null)} onRevoke={onRevoke} />}
  </>;
}
