import type { Session } from '../../api/schemas';
import { describeUserAgent } from '../../lib/describe-user-agent';
import { formatDate } from '../../lib/format-date';

export const connectedAppsMessages = {
  heading: 'Connected AI apps',
  description: (address: string) => `AI apps you connected with OAuth act as you in this workspace, with everything you can do now. To connect one, add ${address} in Claude, ChatGPT, Codex, Cursor or Claude Code: it opens a page here to allow it. Each sign-in is its own row, so two computers show twice.`,
  none: 'No AI app is connected yet.',
  connected: 'Connected',
  lastUsed: 'Last used',
  active: 'Active',
  needsReconnecting: 'Needs reconnecting',
  reconnectSteps: 'To use it again: in Claude or ChatGPT, Disconnect the Colliedesk connector, then Connect; in Claude Code, run /mcp, choose colliedesk and Authenticate; in Codex, run codex mcp login colliedesk; in Cursor, log in to colliedesk again in Settings → MCP.',
  disconnect: 'Disconnect',
  removingInApp: 'Removing the server in the app does not disconnect it here; Disconnect does.',
} as const;
const text = connectedAppsMessages;

interface ConnectedAppsSectionProps {
  // Every session of the list; the section shows the ones an AI app holds (app is set).
  sessions: Session[];
  timezone: string;
  // mcpAddress is this server's MCP address, shown as what to add in the app.
  mcpAddress: string;
  onDisconnect?: (id: string) => void;
}

// ConnectedAppsSection lists the AI apps connected with OAuth, one row per sign-in (device), and
// disconnects one at once. A row that no longer works says how to reconnect in each app.
export function ConnectedAppsSection({ sessions, timezone, mcpAddress, onDisconnect }: ConnectedAppsSectionProps) {
  const apps = sessions.filter((session) => session.app);
  return (
    <section className="settings-card" aria-labelledby="connected-apps-heading">
      <div className="section-heading"><h2 id="connected-apps-heading">{text.heading}</h2><p>{text.description(mcpAddress)}</p></div>
      <div className="settings-body">
        {apps.length === 0 ? <p className="muted-copy">{text.none}</p> : <div className="data-list">{apps.map((session) => {
          const app = session.app!;
          const stale = app.needs_reconnecting || session.revoked_at !== null || Date.parse(session.expires_at) <= Date.now();
          const device = describeUserAgent(app.user_agent);
          return <article key={session.id}>
            <div><span className="session-name"><strong>{session.label}</strong>{device && <span>{device}</span>}</span>
              <span>{session.agent_email}</span>
              <span className={stale ? 'badge badge-warn' : 'badge'}>{stale ? text.needsReconnecting : text.active}</span>
              <span>{text.connected} <time dateTime={app.connected_at}>{formatDate(app.connected_at, timezone)}</time></span>
              <span>{text.lastUsed} <time dateTime={session.last_seen_at}>{formatDate(session.last_seen_at, timezone)}</time></span>
              {stale && <span className="muted-copy">{text.reconnectSteps}</span>}</div>
            {!stale && <button type="button" className="danger-action" onClick={() => onDisconnect?.(session.id)}
              aria-label={`${text.disconnect} ${session.label}`}>{text.disconnect}</button>}
          </article>;
        })}</div>}
        {apps.length > 0 && <p className="muted-copy">{text.removingInApp}</p>}
      </div>
    </section>
  );
}
