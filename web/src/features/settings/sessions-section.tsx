import type { Session } from '../../api/schemas';
import { describeUserAgent } from '../../lib/describe-user-agent';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';
import { permissionLabel } from './token-permissions';

function sessionState(session: Session): keyof typeof settingsMessages.sessionStates {
  if (session.revoked_at) return 'revoked';
  return Date.parse(session.expires_at) <= Date.now() ? 'expired' : 'active';
}

interface SessionsSectionProps {
  sessions: Session[];
  timezone: string;
  onRevokeSession?: (id: string) => void;
}

// SessionsSection names each device by browser and system; revoking this device is signing out, so it says so.
export function SessionsSection({ sessions, timezone, onRevokeSession }: SessionsSectionProps) {
  return (
    <section className="settings-card" aria-labelledby="sessions-heading"><div className="section-heading"><h2 id="sessions-heading">{messages.sessions}</h2><p>{settingsMessages.sessionsDescription}</p></div>
      <div className="settings-body"><div className="data-list">{sessions.map((session) => {
        const state = sessionState(session);
        return <article key={session.id}>
          <div><span className="session-name"><strong title={session.label}>{describeUserAgent(session.label) ?? session.label}</strong>
              {session.current && <span className="badge">{settingsMessages.thisDevice}</span>}</span>
            <span>{session.agent_email}</span>
            <span>{settingsMessages.sessionStates[state]}</span>
            <time>{formatDate(session.last_seen_at, timezone)}</time>
            {session.scope && <span className="token-detail"><span className="badge" title={settingsMessages.tokenTierTitle}>{session.scope === 'custom' ? settingsMessages.customToken(session.permissions.length) : session.scope}</span>
              {session.scope === 'custom' && <details className="token-permissions"><summary>{settingsMessages.tokenPermissions}</summary>
                <ul>{session.permissions.map((name) => <li key={name}>{permissionLabel(name)}</li>)}</ul></details>}
              {state === 'active' && <span>{settingsMessages.tokenExpires} <time dateTime={session.expires_at}>{formatDate(session.expires_at, timezone)}</time></span>}</span>}</div>
          {state === 'active' && <button type="button" className="danger-action" onClick={() => onRevokeSession?.(session.id)}>
            {session.current ? messages.signOut : settingsMessages.revoke}</button>}
        </article>;
      })}</div></div>
    </section>
  );
}
