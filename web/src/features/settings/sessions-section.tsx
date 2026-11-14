import type { Session } from '../../api/schemas';
import { describeUserAgent } from '../../lib/describe-user-agent';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';

function sessionState(session: Session): keyof typeof settingsMessages.sessionStates {
  if (session.revoked_at) return 'revoked';
  return Date.parse(session.expires_at) <= Date.now() ? 'expired' : 'active';
}

interface SessionsSectionProps {
  sessions: Session[];
  timezone: string;
  onRevokeSession?: (id: string) => void;
}

// SessionsSection lists the browsers and clients signed in now (Account), each named by browser and system;
// revoking this device is signing out, so it says so. A session that ended (revoked or expired) is not a place
// the person is signed in, so it is not listed. Agent tokens are listed once, on AI & agents
// (AgentTokensSection), and AI apps connected with OAuth by ConnectedAppsSection.
export function SessionsSection({ sessions, timezone, onRevokeSession }: SessionsSectionProps) {
  const shown = sessions.filter((session) => !session.app && !session.token && sessionState(session) === 'active');
  return (
    <section className="settings-card" aria-labelledby="sessions-heading"><div className="section-heading"><h2 id="sessions-heading">{messages.sessions}</h2><p>{settingsMessages.sessionsDescription}</p></div>
      <div className="settings-body settings-rows"><div className="data-list">{shown.map((session) => {
        return <article key={session.id}>
          <div><span className="session-name"><strong title={session.label}>{describeUserAgent(session.label) ?? session.label}</strong>
              {session.current && <span className="badge">{settingsMessages.thisDevice}</span>}</span>
            <span>{session.agent_email}</span>
            <span>{settingsMessages.sessionStates.active}</span>
            <time>{formatDate(session.last_seen_at, timezone)}</time>
            {session.role && <span className="badge">{settingsMessages.limitedSession(session.role.name)}</span>}</div>
          <button type="button" className="danger-action" onClick={() => onRevokeSession?.(session.id)}>
            {session.current ? messages.signOut : settingsMessages.revoke}</button>
        </article>;
      })}</div></div>
    </section>
  );
}
