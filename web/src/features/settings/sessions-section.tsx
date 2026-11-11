import type { Session } from '../../api/schemas';
import { describeUserAgent } from '../../lib/describe-user-agent';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';
import { permissionLabel } from './token-permissions';
import { StopIcon } from './stop-icon';
import './role-pickers.css';

function sessionState(session: Session): keyof typeof settingsMessages.sessionStates {
  if (session.revoked_at) return 'revoked';
  if (Date.parse(session.expires_at) <= Date.now()) return 'expired';
  // A token whose person no longer holds every permission of the role it follows works again once they do:
  // stopped, not active.
  return session.token?.kind === 'role' && !session.token.role_held ? 'stopped' : 'active';
}

interface SessionsSectionProps {
  sessions: Session[];
  timezone: string;
  onRevokeSession?: (id: string) => void;
  // devices lists the browsers and clients a person signed in on (Account); tokens the sessions of agent
  // tokens (AI & agents). AI apps connected with OAuth are in neither: ConnectedAppsSection lists them.
  kind?: 'devices' | 'tokens';
}

// TokenDetail says what an agent token was given: the role it follows (or that it is stopped while its
// person lacks a permission of that role), a fixed list, or the tier of a token made before roles.
function TokenDetail({ session }: { session: Session }) {
  const token = session.token;
  if (!token) return session.role ? <span className="badge">{settingsMessages.limitedSession(session.role.name)}</span> : null;
  const list = <details className="token-permissions"><summary>{settingsMessages.tokenPermissions}</summary>
    <ul>{token.permissions.map((name) => <li key={name}>{permissionLabel(name)}</li>)}</ul></details>;
  if (token.kind === 'role' && token.role && !token.role_held) {
    const help = `stopped-${session.id}`;
    return <span className="token-detail token-stopped"><span className="badge badge-warn" aria-describedby={help}><StopIcon />
      {settingsMessages.stoppedToken(session.agent_name, token.role.name)}</span><span id={help}>{settingsMessages.stoppedHelp}</span></span>;
  }
  if (token.kind === 'role' && token.role) return <span className="token-detail"><span className="badge">{settingsMessages.followsRole(token.role.name)}</span>{list}</span>;
  if (token.kind === 'legacy') {
    const help = `legacy-${session.id}`;
    return <span className="token-detail"><span className="badge" title={settingsMessages.legacyHelp} aria-describedby={help}>{settingsMessages.legacyToken(token.legacy_tier ?? 'custom')}</span>
      <span id={help} className="sr-only">{settingsMessages.legacyHelp}</span>{list}</span>;
  }
  return <span className="token-detail"><span className="badge">{settingsMessages.fixedToken(token.permissions.length)}</span>{list}</span>;
}

// SessionsSection names each device by browser and system; revoking this device is signing out, so it says so.
// AI apps connected with OAuth are listed by ConnectedAppsSection instead.
export function SessionsSection({ sessions, timezone, onRevokeSession, kind = 'devices' }: SessionsSectionProps) {
  const heading = kind === 'tokens' ? 'agent-sessions-heading' : 'sessions-heading';
  const shown = sessions.filter((session) => !session.app && (kind === 'tokens') === Boolean(session.token));
  return (
    <section className="settings-card" aria-labelledby={heading}><div className="section-heading"><h2 id={heading}>{messages.sessions}</h2><p>{settingsMessages.sessionsDescription}</p></div>
      <div className="settings-body settings-rows"><div className="data-list">{shown.map((session) => {
        const state = sessionState(session);
        return <article key={session.id}>
          <div><span className="session-name"><strong title={session.label}>{describeUserAgent(session.label) ?? session.label}</strong>
              {session.current && <span className="badge">{settingsMessages.thisDevice}</span>}</span>
            <span>{session.agent_email}</span>
            <span>{settingsMessages.sessionStates[state]}</span>
            <time>{formatDate(session.last_seen_at, timezone)}</time>
            <TokenDetail session={session} />
            {session.token && (state === 'active' || state === 'stopped') && <span>{settingsMessages.tokenExpires} <time dateTime={session.expires_at}>{formatDate(session.expires_at, timezone)}</time></span>}</div>
          {(state === 'active' || state === 'stopped') && <button type="button" className="danger-action" onClick={() => onRevokeSession?.(session.id)}>
            {session.current ? messages.signOut : settingsMessages.revoke}</button>}
        </article>;
      })}</div></div>
    </section>
  );
}
