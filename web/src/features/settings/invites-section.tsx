import { useRef, useState } from 'react';
import type { Invite, Role } from '../../api/schemas';
import { RoleCheckboxes } from './role-checkboxes';
import { roleMessages } from './roles-messages';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';
import './role-pickers.css';

// inviteState makes every way an invite can end visible: used, revoked or expired.
function inviteState(invite: Invite): keyof typeof settingsMessages.inviteStates {
  if (invite.used_at) return 'accepted';
  if (invite.revoked_at) return 'revoked';
  return Date.parse(invite.expires_at) <= Date.now() ? 'expired' : 'pending';
}

interface InvitesSectionProps {
  invites: Invite[];
  timezone: string;
  canCreateInvites: boolean;
  // canRevokeInvites: Revoke on a pending invite needs team:revoke.
  canRevokeInvites?: boolean;
  createdInvite?: { url?: string };
  // creatingInvite disables Create invite while one is in flight.
  creatingInvite?: boolean;
  // inviteError explains a refused invite, for example a full agent cap.
  inviteError?: string;
  // The roles an invite may give; a role the person may not give is listed, locked.
  roles?: Role[];
  onCreateInvite?: (email: string, roleIds: string[]) => void;
  onRevokeInvite?: (id: string) => void;
}

export function InvitesSection({ invites, timezone, canCreateInvites, canRevokeInvites = false, createdInvite, creatingInvite = false, inviteError, roles = [], onCreateInvite, onRevokeInvite }: InvitesSectionProps) {
  const createdInviteUrl = createdInvite?.url;
  const [email, setEmail] = useState('');
  // An invite gives the Agent role unless the person picks others.
  const agent = roles.find((role) => role.system === 'agent');
  const [picked, setPicked] = useState<string[] | null>(null);
  const chosen = picked ?? (agent ? [agent.id] : []);
  const [picking, setPicking] = useState(false);
  const rolesButton = useRef<HTMLButtonElement>(null);
  const chosenNames = roles.filter((role) => chosen.includes(role.id)).map((role) => role.name);
  return (
    <section className="settings-card" aria-labelledby="invites-heading"><div className="section-heading"><h2 id="invites-heading">{messages.invites}</h2><p>{settingsMessages.invitesDescription}</p></div>
      <div className="settings-body settings-rows">
        {/* Esc closes the picker from the Roles button as well as from inside it, and keeps the focus on the button. */}
        {canCreateInvites && <form className="invite-form" onKeyDown={(event) => { if (event.key === 'Escape' && picking) { setPicking(false); rolesButton.current?.focus(); } }} onSubmit={(event) => { event.preventDefault(); if (chosen.length > 0) onCreateInvite?.(email, chosen); }}>
          <label className="field"><span>{settingsMessages.inviteEmail}</span><input type="email" value={email} onChange={(event) => setEmail(event.target.value)} required /></label>
          <div className="field"><span>{settingsMessages.inviteRole}</span>
            <button ref={rolesButton} type="button" aria-expanded={picking} aria-controls="invite-roles" onClick={() => setPicking((open) => !open)}>{roleMessages.invitesRoles(chosenNames)}</button></div>
          <button className="primary-action" type="submit" disabled={creatingInvite || chosen.length === 0}>{settingsMessages.createInvite}</button>
          {picking && <div id="invite-roles" className="invite-roles">
            <RoleCheckboxes idPrefix="invite-role" roles={roles} chosen={chosen} onChange={setPicked} />
            {chosen.length === 0 && <p className="error-copy" role="alert">{roleMessages.atLeastOne}</p>}
          </div>}
        </form>}
        {canCreateInvites && inviteError && <p role="alert" className="error-copy">{inviteError}</p>}
        {canCreateInvites && createdInvite && !createdInviteUrl && <p role="status">{settingsMessages.inviteLinkShownOnce}</p>}
        {canCreateInvites && createdInviteUrl && <label className="invite-link"><span className="field-label">{settingsMessages.inviteLink}</span>
          <input aria-label={settingsMessages.inviteLink} readOnly value={createdInviteUrl} />
          <small>{settingsMessages.inviteLinkNotice}</small>
        </label>}
        {invites.length > 0 && <div className="data-list">{invites.map((invite) => {
          const state = inviteState(invite);
          return <article key={invite.id}>
            <div><strong>{invite.email}</strong><span>{invite.roles.map((role) => role.name).join(', ')}</span>
              <span>{settingsMessages.inviteStates[state]}</span><time>{formatDate(invite.expires_at, timezone)}</time></div>
            {canRevokeInvites && state === 'pending' && <button type="button" className="danger-action" onClick={() => onRevokeInvite?.(invite.id)}>{settingsMessages.revoke}</button>}
          </article>;
        })}</div>}
      </div>
    </section>
  );
}
