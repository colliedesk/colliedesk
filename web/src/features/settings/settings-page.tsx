import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import type { Invite, Session } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { AppearanceSection } from './appearance-section';

interface SettingsPageProps {
  sessions: Session[];
  invites: Invite[];
  timezone: string;
  canCreateInvites?: boolean;
  canManageWorkspace?: boolean;
  workspaceTimezone?: string;
  timezoneOverride?: string | null;
  createdInviteUrl?: string;
  onCreateInvite?: (email: string, role: 'admin' | 'agent') => void;
  onRevokeSession?: (id: string) => void;
  onRevokeInvite?: (id: string) => void;
  onSetWorkspaceTimezone?: (timezone: string) => void;
  onSetAgentTimezone?: (timezone: string | null) => void;
  error?: string;
  emailSection?: ReactNode;
}

function sessionState(session: Session): keyof typeof messages.sessionStates {
  if (session.revoked_at) return 'revoked';
  return Date.parse(session.expires_at) <= Date.now() ? 'expired' : 'active';
}

// inviteState makes every way an invite can end visible: used, revoked or expired.
function inviteState(invite: Invite): keyof typeof messages.inviteStates {
  if (invite.used_at) return 'accepted';
  if (invite.revoked_at) return 'revoked';
  return Date.parse(invite.expires_at) <= Date.now() ? 'expired' : 'pending';
}

export function SettingsPage({ sessions, invites, timezone, canCreateInvites = false, canManageWorkspace = false,
  workspaceTimezone = 'UTC', timezoneOverride = null, createdInviteUrl, onCreateInvite, onRevokeInvite,
  onRevokeSession, onSetWorkspaceTimezone, onSetAgentTimezone, error, emailSection }: SettingsPageProps) {
  const [email, setEmail] = useState('');
  const [role, setRole] = useState<'admin' | 'agent'>('agent');
  const [workspaceZone, setWorkspaceZone] = useState(workspaceTimezone);
  const [agentZone, setAgentZone] = useState(timezoneOverride ?? '');
  useEffect(() => setWorkspaceZone(workspaceTimezone), [workspaceTimezone]);
  useEffect(() => setAgentZone(timezoneOverride ?? ''), [timezoneOverride]);
  return (
    <main className="settings-page panel">
      <header><h1>{messages.settings}</h1><p>{messages.workspaceSettings}</p></header>
      {error && <p role="alert" className="error-copy">{error}</p>}
      <section className="settings-card"><div className="section-heading"><h2>{messages.timezoneSettings}</h2><p>{messages.timezoneDescription}</p></div>
        <div className="settings-body">
          {canManageWorkspace && <label className="field"><span>{messages.workspaceTimezone}</span><span className="input-with-actions"><input value={workspaceZone} onChange={(event) => setWorkspaceZone(event.target.value)} />
            <button type="button" onClick={() => onSetWorkspaceTimezone?.(workspaceZone)}>{messages.saveWorkspaceTimezone}</button></span></label>}
          <label className="field"><span>{messages.personalTimezoneOverride}</span><span className="input-with-actions"><input value={agentZone} placeholder={workspaceTimezone} onChange={(event) => setAgentZone(event.target.value)} />
            <button type="button" onClick={() => onSetAgentTimezone?.(agentZone)}>{messages.savePersonalTimezone}</button>
              <button type="button" onClick={() => { setAgentZone(''); onSetAgentTimezone?.(null); }}>{messages.clearPersonalTimezone}</button></span></label>
        </div>
      </section>
      {emailSection}
      <AppearanceSection />
      <section className="settings-card" aria-labelledby="sessions-heading"><div className="section-heading"><h2 id="sessions-heading">{messages.sessions}</h2><p>{messages.sessionsDescription}</p></div>
        <div className="settings-body"><div className="data-list">{sessions.map((session) => {
          const state = sessionState(session);
          return <article key={session.id}>
            <div><strong title={session.label}>{session.label}</strong><span>{session.agent_email}</span><span>{messages.sessionStates[state]}</span>
              <time>{formatDate(session.last_seen_at, timezone)}</time></div>
            {state === 'active' && <button type="button" className="danger-action" onClick={() => onRevokeSession?.(session.id)}>{messages.revoke}</button>}
          </article>;
        })}</div></div>
      </section>
      <section className="settings-card" aria-labelledby="invites-heading"><div className="section-heading"><h2 id="invites-heading">{messages.invites}</h2><p>{messages.invitesDescription}</p></div>
        <div className="settings-body">
          {canCreateInvites && <form className="invite-form" onSubmit={(event) => { event.preventDefault(); onCreateInvite?.(email, role); }}>
            <label className="field"><span>{messages.inviteEmail}</span><input type="email" value={email} onChange={(event) => setEmail(event.target.value)} required /></label>
            <label className="field"><span>{messages.roleCeiling}</span><select value={role} onChange={(event) => setRole(event.target.value as 'admin' | 'agent')}>
              <option value="agent">{messages.roles.agent}</option><option value="admin">{messages.roles.admin}</option>
            </select></label>
            <button className="primary-action" type="submit">{messages.createInvite}</button>
          </form>}
          {canCreateInvites && createdInviteUrl && <label className="invite-link"><span className="field-label">{messages.inviteLink}</span>
            <input aria-label={messages.inviteLink} readOnly value={createdInviteUrl} />
            <small>{messages.inviteLinkNotice}</small>
          </label>}
          {invites.length > 0 && <div className="data-list">{invites.map((invite) => {
            const state = inviteState(invite);
            return <article key={invite.id}>
              <div><strong>{invite.email}</strong><span>{messages.roles[invite.role_ceiling]}</span>
                <span>{messages.inviteStates[state]}</span><time>{formatDate(invite.expires_at, timezone)}</time></div>
              {canCreateInvites && state === 'pending' && <button type="button" className="danger-action" onClick={() => onRevokeInvite?.(invite.id)}>{messages.revoke}</button>}
            </article>;
          })}</div>}
        </div>
      </section>
    </main>
  );
}
