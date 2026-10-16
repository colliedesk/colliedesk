import type { Session } from '../../api/schemas';
import { describeUserAgent } from '../../lib/describe-user-agent';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';

function sessionState(session: Session): keyof typeof messages.sessionStates {
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
    <section className="settings-card" aria-labelledby="sessions-heading"><div className="section-heading"><h2 id="sessions-heading">{messages.sessions}</h2><p>{messages.sessionsDescription}</p></div>
      <div className="settings-body"><div className="data-list">{sessions.map((session) => {
        const state = sessionState(session);
        return <article key={session.id}>
          <div><span className="session-name"><strong title={session.label}>{describeUserAgent(session.label) ?? session.label}</strong>
              {session.current && <span className="badge">{messages.thisDevice}</span>}</span>
            <span>{session.agent_email}</span>
            <span>{messages.sessionStates[state]}</span>
            <time>{formatDate(session.last_seen_at, timezone)}</time></div>
          {state === 'active' && <button type="button" className="danger-action" onClick={() => onRevokeSession?.(session.id)}>
            {session.current ? messages.signOut : messages.revoke}</button>}
        </article>;
      })}</div></div>
    </section>
  );
}
