import { useState } from 'react';
import type { Invite } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';

// inviteState makes every way an invite can end visible: used, revoked or expired.
function inviteState(invite: Invite): keyof typeof messages.inviteStates {
  if (invite.used_at) return 'accepted';
  if (invite.revoked_at) return 'revoked';
  return Date.parse(invite.expires_at) <= Date.now() ? 'expired' : 'pending';
}

interface InvitesSectionProps {
  invites: Invite[];
  timezone: string;
  canCreateInvites: boolean;
  createdInvite?: { url?: string };
  // creatingInvite disables Create invite while one is in flight.
  creatingInvite?: boolean;
  // inviteError explains a refused invite, for example a full agent cap.
  inviteError?: string;
  onCreateInvite?: (email: string, role: 'admin' | 'agent') => void;
  onRevokeInvite?: (id: string) => void;
}

export function InvitesSection({ invites, timezone, canCreateInvites, createdInvite, creatingInvite = false, inviteError, onCreateInvite, onRevokeInvite }: InvitesSectionProps) {
  const createdInviteUrl = createdInvite?.url;
  const [email, setEmail] = useState('');
  const [role, setRole] = useState<'admin' | 'agent'>('agent');
  return (
    <section className="settings-card" aria-labelledby="invites-heading"><div className="section-heading"><h2 id="invites-heading">{messages.invites}</h2><p>{messages.invitesDescription}</p></div>
      <div className="settings-body">
        {canCreateInvites && <form className="invite-form" onSubmit={(event) => { event.preventDefault(); onCreateInvite?.(email, role); }}>
          <label className="field"><span>{messages.inviteEmail}</span><input type="email" value={email} onChange={(event) => setEmail(event.target.value)} required /></label>
          <label className="field"><span>{messages.inviteRole}</span><select value={role} onChange={(event) => setRole(event.target.value as 'admin' | 'agent')}>
            <option value="agent">{messages.roles.agent}</option><option value="admin">{messages.roles.admin}</option>
          </select></label>
          <button className="primary-action" type="submit" disabled={creatingInvite}>{messages.createInvite}</button>
        </form>}
        {canCreateInvites && inviteError && <p role="alert" className="error-copy">{inviteError}</p>}
        {canCreateInvites && createdInvite && !createdInviteUrl && <p role="status">{messages.inviteLinkShownOnce}</p>}
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
  );
}
