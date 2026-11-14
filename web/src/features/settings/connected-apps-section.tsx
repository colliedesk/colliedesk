import type { Session } from '../../api/schemas';
import { describeUserAgent } from '../../lib/describe-user-agent';
import { formatDate } from '../../lib/format-date';
import { HelpTip } from '../../ui/help-tip';

export const connectedAppsMessages = {
  heading: 'Connected AI apps',
  description: 'AI apps you connected act as you in this workspace.',
  // The address is a value to copy, so its line stays; the rest of how to connect is in the tip.
  connectAddress: (address: string) => `To connect one, add ${address} in your AI app.`,
  descriptionWhy: (address: string) => `AI apps you connected with OAuth act as you in this workspace, with everything you can do now. To connect one, add ${address} in Claude, ChatGPT, Codex, Cursor or Claude Code: it opens a page here to allow it. Each sign-in is its own row, so two computers show twice.`,
  none: 'No AI app is connected yet.',
  connected: 'Connected',
  lastUsed: 'Last used',
  active: 'Active',
  needsReconnecting: 'Needs reconnecting',
  reconnectSteps: 'To use it again: in Claude or ChatGPT, Disconnect the Colliedesk connector, then Connect; in Claude Code, run /mcp, choose colliedesk (plugin:colliedesk:colliedesk with the Colliedesk plugin) and Authenticate; in Codex, run codex mcp login colliedesk; in Cursor, log in to colliedesk again in Settings → MCP.',
  disconnect: 'Disconnect',
  removingInApp: 'Removing it in the app does not disconnect it here.',
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
      <div className="section-heading"><div className="heading-with-tip"><h2 id="connected-apps-heading">{text.heading}</h2><HelpTip label={text.heading} text={text.descriptionWhy(mcpAddress)} /></div><p>{text.description}</p></div>
      <div className="settings-body settings-rows">
        <p className="muted-copy" data-quiet="value">{text.connectAddress(mcpAddress)}</p>
        {apps.length === 0 ? <p className="muted-copy">{text.none}</p> : <div className="data-list">{apps.map((session) => {
          const app = session.app!;
          const stale = app.needs_reconnecting || session.revoked_at !== null || Date.parse(session.expires_at) <= Date.now();
          const device = describeUserAgent(app.user_agent);
          return <article key={session.id}>
            <div><span className="session-name"><strong title={session.label}>{session.label}</strong></span>
              <span title={session.agent_email}>{session.agent_email}</span>
              <span className={stale ? 'badge badge-warn' : 'badge'}>{stale ? text.needsReconnecting : text.active}</span>
              <span>{text.lastUsed} <time dateTime={session.last_seen_at}>{formatDate(session.last_seen_at, timezone)}</time></span>
              <p className="app-detail">{device && <>{device} · </>}{text.connected} <time dateTime={app.connected_at}>{formatDate(app.connected_at, timezone)}</time></p>
              {stale && <p className="reconnect-steps">{text.reconnectSteps}</p>}</div>
            {!stale && <button type="button" className="danger-action" onClick={() => onDisconnect?.(session.id)}
              aria-label={`${text.disconnect} ${session.label}`}>{text.disconnect}</button>}
          </article>;
        })}</div>}
        {apps.length > 0 && <p className="muted-copy">{text.removingInApp}</p>}
      </div>
    </section>
  );
}
