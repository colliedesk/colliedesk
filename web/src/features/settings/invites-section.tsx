import { useEffect, useRef, useState } from 'react';
import type { Invite, Role } from '../../api/schemas';
import { RoleCheckboxes } from './role-checkboxes';
import { RowMenu } from './row-menu';
import type { RowMenuItem } from './row-menu';
import { roleMessages } from './roles-messages';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { HelpTip } from '../../ui/help-tip';
import { settingsMessages } from './settings-messages';
import { useEndedAnnouncement } from './use-ended-announcement';
import { useInviteDraft } from './use-invite-draft';
import type { InviteDraft } from './use-invite-draft';
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
  // myPermissions is what the person holds: an invite whose roles hold more is refused by the server, so no new link is offered for it.
  myPermissions?: readonly string[];
  onCreateInvite?: (email: string, roleIds: string[]) => void;
  onRevokeInvite?: (id: string) => void;
  // A new link for the invite's address and roles; mayRevoke says the old pending link may be revoked after it. Resolves to
  // whether the link was made. inviteNotice says what is true afterwards.
  onResendInvite?: (invite: Invite, mayRevoke: boolean) => Promise<boolean>;
  inviteNotice?: { kind: 'status' | 'alert'; text: string };
  // draft keeps the typed invite where the page can hold it; without one the form keeps its own.
  draft?: InviteDraft;
}

export function InvitesSection({ invites, timezone, canCreateInvites, canRevokeInvites = false, createdInvite, creatingInvite = false, inviteError, roles = [], myPermissions, onCreateInvite, onRevokeInvite, onResendInvite, inviteNotice, draft }: InvitesSectionProps) {
  const createdInviteUrl = createdInvite?.url;
  const own = useInviteDraft();
  const { email, setEmail, picked, setPicked } = draft ?? own;
  // An invite gives the Agent role unless the person picks others.
  const agent = roles.find((role) => role.system === 'agent');
  const chosen = picked ?? (agent ? [agent.id] : []);
  const [picking, setPicking] = useState(false);
  const rolesButton = useRef<HTMLButtonElement>(null);
  // A new link for a row takes the focus to its field, where the one-time link is, and the page says what happened.
  const linkField = useRef<HTMLInputElement>(null);
  const focusLink = useRef(false);
  useEffect(() => {
    if (focusLink.current && createdInviteUrl) { linkField.current?.focus(); focusLink.current = false; }
  }, [createdInviteUrl]);
  const newLink = (invite: Invite) => {
    focusLink.current = true;
    void onResendInvite?.(invite, canRevokeInvites).then((made) => { if (!made) focusLink.current = false; });
  };
  const revoked = useEndedAnnouncement(invites, (invite) => invite.id, (invite) => inviteState(invite) === 'revoked', (invite) => settingsMessages.inviteRevoked(invite.email));
  // The server refuses an invite whose roles hold a permission the person lacks (role_exceeds_yours): the menu does not offer a
  // new link for one. A role the list does not hold, or no list of what the person holds, is left to the server, whose reason
  // shows where a refused invite shows it.
  const mayGive = (invite: Invite) => !myPermissions || invite.roles.every((held) => {
    const role = roles.find((candidate) => candidate.id === held.id);
    return !role || role.permissions.every((name) => myPermissions.includes(name));
  });
  const chosenNames = roles.filter((role) => chosen.includes(role.id)).map((role) => role.name);
  return (
    <section className="settings-card" aria-labelledby="invites-heading"><div className="section-heading"><div className="heading-with-tip"><h2 id="invites-heading">{messages.invites}</h2><HelpTip label={messages.invites} text={settingsMessages.invitesWhy} /></div><p>{settingsMessages.invitesDescription}</p></div>
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
        <p role="status" className="sr-only">{revoked}</p>
        {canCreateInvites && inviteNotice && <p role={inviteNotice.kind === 'alert' ? 'alert' : 'status'} className={inviteNotice.kind === 'alert' ? 'error-copy' : 'muted-copy'}>{inviteNotice.text}</p>}
        {canCreateInvites && createdInvite && !createdInviteUrl && <p role="status">{settingsMessages.inviteLinkShownOnce}</p>}
        {canCreateInvites && createdInviteUrl && <label className="invite-link"><span className="field-label">{settingsMessages.inviteLink}</span>
          <input ref={linkField} aria-label={settingsMessages.inviteLink} readOnly value={createdInviteUrl} />
          <small>{settingsMessages.inviteLinkNotice}</small>
        </label>}
        {invites.length > 0 && <div className="data-list">{invites.map((invite) => {
          const state = inviteState(invite);
          const live = state === 'pending' || state === 'expired';
          const actions: RowMenuItem[] = [
            ...(canCreateInvites && live && mayGive(invite) ? [{ key: 'new-link', label: settingsMessages.newInviteLink, onSelect: () => newLink(invite) }] : []),
            ...(canRevokeInvites && state === 'pending' ? [{ key: 'revoke', label: settingsMessages.revoke, danger: true, onSelect: () => onRevokeInvite?.(invite.id) }] : []),
          ];
          return <article key={invite.id}>
            <div><strong>{invite.email}</strong><span>{invite.roles.map((role) => role.name).join(', ')}</span>
              <span>{settingsMessages.inviteStates[state]}</span><time>{formatDate(invite.expires_at, timezone)}</time></div>
            {actions.length > 0 && <RowMenu label={invite.email} items={actions} />}
          </article>;
        })}</div>}
      </div>
    </section>
  );
}
