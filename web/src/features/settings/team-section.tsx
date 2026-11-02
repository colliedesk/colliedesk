import { useEffect, useRef, useState } from 'react';
import type { Role, TeamMember } from '../../api/schemas';
import { RoleCheckboxes } from './role-checkboxes';
import { roleMessages } from './roles-messages';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';
import { displayNameMaxLength } from './display-name-field';

interface TeamSectionProps {
  members: TeamMember[];
  currentAgentId?: string;
  // canManage: the session holds team:manage_roles; each member says whether it may change them (manageable).
  canManage: boolean;
  // The workspace's roles, to give; myPermissions is what the session may do, for the warnings.
  roles?: Role[];
  myPermissions?: readonly string[];
  timezone: string;
  error?: string;
  // The whole list of a member's roles.
  onSetRoles?: (id: string, roleIds: string[]) => Promise<unknown>;
  // name is trimmed; an empty name clears it and the email is shown again.
  onRename?: (id: string, name: string) => void;
  // reassignTo is the agent that takes the open and pending conversations, or null for Unassigned.
  onDeactivate?: (id: string, reassignTo: string | null) => void;
  onReactivate?: (id: string) => void;
}

const nameOf = (member: TeamMember) => member.display_name || member.email;

export function TeamSection({ members, currentAgentId, canManage, roles = [], myPermissions = [], timezone, error, onSetRoles, onRename, onDeactivate, onReactivate }: TeamSectionProps) {
  const [confirming, setConfirming] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [changing, setChanging] = useState<string | null>(null);
  const [status, setStatus] = useState<string>();
  // After a question or the rename form closes, focus goes to the row (done) or the button that opened it (cancelled).
  const [refocus, setRefocus] = useState<{ id: string; target: 'row' | 'button' | 'rename' | 'roles' } | null>(null);
  const rows = useRef(new Map<string, HTMLElement>());
  const buttons = useRef(new Map<string, HTMLElement>());
  const renameButtons = useRef(new Map<string, HTMLElement>());
  const roleButtons = useRef(new Map<string, HTMLElement>());
  useEffect(() => {
    if (!refocus) return;
    ({ row: rows, button: buttons, rename: renameButtons, roles: roleButtons })[refocus.target].current.get(refocus.id)?.focus();
    setRefocus(null);
  }, [refocus]);
  // The server refuses to leave a workspace without an active member holding Admin; the button is not offered either.
  const isAdmin = (member: TeamMember) => member.roles.some((role) => role.system === 'admin');
  const activeAdmins = members.filter((member) => isAdmin(member) && member.deactivated_at === null).length;
  return (
    <section className="settings-card" aria-labelledby="team-heading">
      <div className="section-heading"><h2 id="team-heading">{messages.team}</h2><p>{settingsMessages.teamDescription}</p></div>
      <div className="settings-body">
        {error && <p role="alert" className="error-copy">{error}</p>}
        <p role="status" className="sr-only">{status}</p>
        <div className="data-list team-list">{members.map((member) => {
          const name = nameOf(member);
          const deactivated = member.deactivated_at !== null;
          const lastAdmin = isAdmin(member) && !deactivated && activeAdmins === 1;
          const manage = canManage && member.manageable;
          return <article key={member.id} tabIndex={-1} ref={(node) => { if (node) rows.current.set(member.id, node); else rows.current.delete(member.id); }}>
            <div className="team-cells">
              <span className="team-person"><strong title={member.email}>{name}{member.id === currentAgentId && <small> ({settingsMessages.teamYou})</small>}</strong>
                {member.display_name && <small>{member.email}</small>}</span>
              <span className="team-role role-chips" aria-label={settingsMessages.teamRoleOf(name)}>
                {member.roles.map((role) => <span key={role.id} className="role-chip">{role.name}</span>)}
                {manage && !deactivated && changing !== member.id && <button type="button" aria-expanded={false} aria-controls={`roles-of-${member.id}`}
                  ref={(node) => { if (node) roleButtons.current.set(member.id, node); else roleButtons.current.delete(member.id); }}
                  aria-label={`${roleMessages.changeRoles}: ${name}`} onClick={() => { setStatus(undefined); setChanging(member.id); }}>{roleMessages.changeRoles}</button>}
              </span>
              <span>{deactivated ? settingsMessages.teamDeactivated : settingsMessages.teamActive}</span>
              <time>{member.last_active_at ? settingsMessages.teamLastActive(formatDate(member.last_active_at, timezone)) : settingsMessages.teamNeverSignedIn}</time>
            </div>
            {canManage && !manage && <span className="team-actions" aria-label={roleMessages.outranks} />}
            {manage && <span className="team-actions">
              {renaming !== member.id &&
                <button type="button" aria-label={settingsMessages.teamRenameName(name)} ref={(node) => { if (node) renameButtons.current.set(member.id, node); else renameButtons.current.delete(member.id); }}
                  onClick={() => { setConfirming(null); setRenaming(member.id); }}>{settingsMessages.teamRename}</button>}
              {!deactivated && !lastAdmin && confirming !== member.id &&
                <button type="button" className="danger-action" aria-label={settingsMessages.teamDeactivateName(name)} ref={(node) => { if (node) buttons.current.set(member.id, node); else buttons.current.delete(member.id); }} onClick={() => { setRenaming(null); setConfirming(member.id); }}>{settingsMessages.teamDeactivate}</button>}
              {deactivated &&
                <button type="button" aria-label={settingsMessages.teamReactivateName(name)} title={settingsMessages.teamReactivateHelp} onClick={() => onReactivate?.(member.id)}>{settingsMessages.teamReactivate}</button>}
            </span>}
            {changing === member.id && <ChangeRoles member={member} roles={roles} myPermissions={myPermissions} lastAdmin={lastAdmin}
              isSelf={member.id === currentAgentId}
              onCancel={() => { setChanging(null); setRefocus({ id: member.id, target: 'roles' }); }}
              onApply={async (roleIds) => {
                await onSetRoles?.(member.id, roleIds);
                setChanging(null);
                setStatus(roleMessages.nowHas(name, roles.filter((role) => roleIds.includes(role.id)).map((role) => role.name)));
                setRefocus({ id: member.id, target: 'roles' });
              }} />}
            {renaming === member.id && <RenameForm member={member}
              onCancel={() => { setRenaming(null); setRefocus({ id: member.id, target: 'rename' }); }}
              onSave={(next) => { setRenaming(null); setRefocus({ id: member.id, target: 'row' }); onRename?.(member.id, next); }} />}
            {confirming === member.id && <DeactivateConfirm member={member} others={members.filter((other) => other.id !== member.id && other.deactivated_at === null)}
              isSelf={member.id === currentAgentId}
              onCancel={() => { setConfirming(null); setRefocus({ id: member.id, target: 'button' }); }}
              onConfirm={(reassignTo) => { setConfirming(null); setRefocus({ id: member.id, target: 'row' }); onDeactivate?.(member.id, reassignTo); }} />}
          </article>;
        })}</div>
      </div>
    </section>
  );
}

interface DeactivateConfirmProps {
  member: TeamMember;
  others: TeamMember[];
  isSelf: boolean;
  onCancel: () => void;
  onConfirm: (reassignTo: string | null) => void;
}

function DeactivateConfirm({ member, others, isSelf, onCancel, onConfirm }: DeactivateConfirmProps) {
  const name = nameOf(member);
  const [assign, setAssign] = useState(false);
  const [target, setTarget] = useState(others[0]?.id ?? '');
  const hasWork = member.open_conversations > 0;
  const group = useRef<HTMLDivElement>(null);
  useEffect(() => group.current?.focus(), []);
  const headingId = `deactivate-${member.id}`;
  return (
    <div role="group" aria-labelledby={headingId} className="team-confirm" tabIndex={-1} ref={group}>
      <strong id={headingId}>{settingsMessages.teamDeactivateQuestion(name)}</strong>
      <p>{settingsMessages.teamOpenWork(member.open_conversations)} {settingsMessages.teamKeepsHistory}{isSelf && ` ${settingsMessages.teamSignedOut}`}</p>
      {hasWork && <>
        <label><input type="radio" name={`reassign-${member.id}`} checked={!assign} onChange={() => setAssign(false)} /> {settingsMessages.teamMoveToUnassigned}</label>
        <label><input type="radio" name={`reassign-${member.id}`} checked={assign} disabled={others.length === 0} onChange={() => setAssign(true)} /> {settingsMessages.teamAssignToAnother}</label>
        {assign && <select aria-label={settingsMessages.teamAssignTo} value={target} onChange={(event) => setTarget(event.target.value)}>
          {others.map((other) => <option key={other.id} value={other.id}>{nameOf(other)}</option>)}
        </select>}
      </>}
      <span className="button-row">
        <button type="button" className="danger-action" onClick={() => onConfirm(hasWork && assign ? target : null)}>{settingsMessages.teamConfirmDeactivate}</button>
        <button type="button" onClick={onCancel}>{settingsMessages.teamCancel}</button>
      </span>
    </div>
  );
}

interface ChangeRolesProps {
  member: TeamMember;
  roles: Role[];
  myPermissions: readonly string[];
  lastAdmin: boolean;
  isSelf: boolean;
  onCancel: () => void;
  onApply: (roleIds: string[]) => Promise<unknown>;
}

// ChangeRoles is the list of roles under a member's row: tick the roles they should hold, then Apply.
// Taking a role a token of theirs follows stops that token (said before, with its name), and taking
// Change roles from yourself is said too.
function ChangeRoles({ member, roles, myPermissions, lastAdmin, isSelf, onCancel, onApply }: ChangeRolesProps) {
  const name = nameOf(member);
  const [chosen, setChosen] = useState(member.roles.map((role) => role.id));
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string>();
  const group = useRef<HTMLDivElement>(null);
  useEffect(() => { group.current?.querySelector<HTMLInputElement>('input')?.focus(); }, []);
  const admin = roles.find((role) => role.system === 'admin');
  const locked = new Map<string, string>();
  if (lastAdmin && admin) locked.set(admin.id, settingsMessages.teamLastAdmin);
  const removed = member.roles.filter((role) => !chosen.includes(role.id)).map((role) => role.id);
  const stopping = roles.filter((role) => removed.includes(role.id)).flatMap((role) => role.tokens.filter((token) => token.owner === name).map((token) => token.label));
  const keepsManage = chosen.some((id) => { const role = roles.find((item) => item.id === id); return role?.system === 'admin' || role?.permissions.includes('team:manage_roles'); });
  const losesManage = isSelf && myPermissions.includes('team:manage_roles') && !keepsManage;
  const apply = async () => {
    setBusy(true);
    setFailed(undefined);
    try {
      await onApply(chosen);
    } catch (error) {
      setFailed(error instanceof Error && error.message ? error.message : roleMessages.saveFailed);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div id={`roles-of-${member.id}`} role="group" aria-label={roleMessages.rolesOf(name)} className="team-roles" ref={group}
      onKeyDown={(event) => { if (event.key === 'Escape') { event.preventDefault(); onCancel(); } }}>
      <RoleCheckboxes idPrefix={`member-${member.id}`} roles={roles} chosen={chosen} onChange={setChosen} locked={locked}
        note={(role) => {
          const tokens = role.tokens.filter((token) => token.owner === name).map((token) => token.label);
          return removed.includes(role.id) && tokens.length > 0 ? <p className="warning-note">{roleMessages.tokensStop(name, tokens)}</p> : null;
        }} />
      {losesManage && <p className="warning-note">{roleMessages.loseManageTeam}</p>}
      {chosen.length === 0 && <p className="error-copy">{roleMessages.atLeastOne}</p>}
      {failed && <p role="alert" className="error-copy">{failed}</p>}
      <span className="button-row">
        <button type="button" className="primary-action" disabled={busy || chosen.length === 0} onClick={() => void apply()}>
          {stopping.length > 0 ? roleMessages.removeAndStop(stopping.length) : roleMessages.apply}</button>
        <button type="button" onClick={onCancel}>{settingsMessages.teamCancel}</button>
      </span>
    </div>
  );
}

function RenameForm({ member, onCancel, onSave }: { member: TeamMember; onCancel: () => void; onSave: (name: string) => void }) {
  const [draft, setDraft] = useState(member.display_name);
  return (
    <form className="form-row team-rename" onSubmit={(event) => { event.preventDefault(); onSave(draft.trim()); }}>
      {/* The field opens because the person asked for it, so it takes focus at once. */}
      <input aria-label={settingsMessages.teamDisplayNameOf(nameOf(member))} value={draft} maxLength={displayNameMaxLength} placeholder={member.email} autoFocus
        onChange={(event) => setDraft(event.target.value)} onKeyDown={(event) => { if (event.key === 'Escape') onCancel(); }} />
      <span className="button-row">
        <button type="submit">{settingsMessages.teamSaveName}</button>
        <button type="button" onClick={onCancel}>{settingsMessages.teamCancel}</button>
      </span>
    </form>
  );
}
