import { useEffect, useRef, useState } from 'react';
import { InvalidRequestError, PlanLimitError } from '../../api/client';
import type { Role, TeamMember } from '../../api/schemas';
import { RoleCheckboxes } from './role-checkboxes';
import { RowMenu } from './row-menu';
import { roleMessages } from './roles-messages';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { HelpTip } from '../../ui/help-tip';
import { useConfirmDrop, useDraftIds, useDropDrafts, useSaveChanges, useSettingsDraft } from './settings-edits';
import { settingsMessages } from './settings-messages';
import { displayNameMaxLength } from './display-name-field';
import './role-pickers.css';

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
  // onRename resolves once the server took the name and rejects with its reason; the form stays open until then.
  onRename?: (id: string, name: string) => Promise<unknown> | void;
  // reassignTo is the agent that takes the open and pending conversations, or null for Unassigned.
  onDeactivate?: (id: string, reassignTo: string | null) => void;
  onReactivate?: (id: string) => void;
}

const nameOf = (member: TeamMember) => member.display_name || member.email;

export function TeamSection({ members, currentAgentId, canManage, roles = [], myPermissions = [], timezone, error, onSetRoles, onRename, onDeactivate, onReactivate }: TeamSectionProps) {
  const [confirming, setConfirming] = useState<string | null>(null);
  const [renamingNow, setRenaming] = useState<string | null>(null);
  // A name typed on a tab that was left is still the page's: the form opens again on that row.
  const draftRows = useDraftIds('rename:');
  const renameable = (id: string) => canManage && members.some((member) => member.id === id && member.manageable);
  const draftRow = draftRows.map((id) => id.slice('rename:'.length)).find(renameable);
  const renaming = (renamingNow && renameable(renamingNow) ? renamingNow : null) ?? draftRow ?? null;
  const confirmDrop = useConfirmDrop();
  const dropDrafts = useDropDrafts();
  // A draft for someone the person can no longer rename (their role changed, they left) is not kept.
  const stale = draftRows.filter((id) => !renameable(id.slice('rename:'.length))).join();
  useEffect(() => { for (const id of stale ? stale.split(',') : []) dropDrafts(id); }, [stale]);
  // Opening another row's editor, or a question, closes the rename form: its typed name is not dropped without asking.
  const leaveRename = () => { if (renaming && !confirmDrop(`rename:${renaming}`)) return false; dropDrafts('rename:'); return true; };
  const [changing, setChanging] = useState<string | null>(null);
  const [status, setStatus] = useState<string>();
  // After a question or the rename form closes, focus goes to the row (done) or the menu that opened it (cancelled).
  const [refocus, setRefocus] = useState<{ id: string; target: 'row' | 'menu' } | null>(null);
  const rows = useRef(new Map<string, HTMLElement>());
  // Every action of a row is in its "⋯" menu: when one closes or is cancelled, focus goes back to that button.
  const menuButtons = useRef(new Map<string, HTMLElement>());
  useEffect(() => {
    if (!refocus) return;
    (refocus.target === 'row' ? rows : menuButtons).current.get(refocus.id)?.focus();
    setRefocus(null);
  }, [refocus]);
  // The server refuses to leave a workspace without an active member holding Admin; the button is not offered either.
  const isAdmin = (member: TeamMember) => member.roles.some((role) => role.system === 'admin');
  const activeAdmins = members.filter((member) => isAdmin(member) && member.deactivated_at === null).length;
  return (
    <section className="settings-card" aria-labelledby="team-heading">
      <div className="section-heading"><div className="heading-with-tip"><h2 id="team-heading">{messages.team}</h2><HelpTip label={messages.team} text={settingsMessages.teamWhy} /></div><p>{settingsMessages.teamDescription}</p></div>
      <div className="settings-body settings-rows">
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
                {member.roles.map((role) => <span key={role.id} className="role-chip" title={role.name}>{role.name}</span>)}
              </span>
              <span>{deactivated ? settingsMessages.teamDeactivated : settingsMessages.teamActive}</span>
              <time>{member.last_active_at ? settingsMessages.teamLastActive(formatDate(member.last_active_at, timezone)) : settingsMessages.teamNeverSignedIn}</time>
            </div>
            {canManage && !manage && <span className="team-actions" aria-label={roleMessages.outranks} />}
            {manage && <span className="team-actions"><RowMenu label={name} buttonRef={(node) => { if (node) menuButtons.current.set(member.id, node); else menuButtons.current.delete(member.id); }}
              items={[
                ...(!deactivated && changing !== member.id ? [{ key: 'roles', label: roleMessages.changeRoles, name: `${roleMessages.changeRoles}: ${name}`,
                  onSelect: () => { setStatus(undefined); setChanging(member.id); } }] : []),
                ...(renaming !== member.id ? [{ key: 'rename', label: settingsMessages.teamRename, name: settingsMessages.teamRenameName(name),
                  onSelect: () => { if (!leaveRename()) return; setConfirming(null); setRenaming(member.id); } }] : []),
                ...(!deactivated && !lastAdmin && confirming !== member.id ? [{ key: 'deactivate', label: settingsMessages.teamDeactivate, name: settingsMessages.teamDeactivateName(name), danger: true,
                  onSelect: () => { if (!leaveRename()) return; setRenaming(null); setConfirming(member.id); } }] : []),
                ...(deactivated ? [{ key: 'reactivate', label: settingsMessages.teamReactivate, name: settingsMessages.teamReactivateName(name), onSelect: () => { onReactivate?.(member.id); setStatus(settingsMessages.teamReactivated(name)); } }] : []),
              ]} /></span>}
            {changing === member.id && <ChangeRoles member={member} roles={roles} myPermissions={myPermissions} lastAdmin={lastAdmin}
              isSelf={member.id === currentAgentId}
              onCancel={() => { setChanging(null); setRefocus({ id: member.id, target: 'menu' }); }}
              onApply={async (roleIds) => {
                await onSetRoles?.(member.id, roleIds);
                setChanging(null);
                setStatus(roleMessages.nowHas(name, roles.filter((role) => roleIds.includes(role.id)).map((role) => role.name)));
                setRefocus({ id: member.id, target: 'menu' });
              }} />}
            {renaming === member.id && <RenameForm member={member}
              onCancel={() => { setRenaming(null); setRefocus({ id: member.id, target: 'menu' }); }}
              onSave={async (next) => { await onRename?.(member.id, next); setRenaming(null); setRefocus({ id: member.id, target: 'row' }); }} />}
            {confirming === member.id && <DeactivateConfirm member={member} others={members.filter((other) => other.id !== member.id && other.deactivated_at === null)}
              isSelf={member.id === currentAgentId}
              onCancel={() => { setConfirming(null); setRefocus({ id: member.id, target: 'menu' }); }}
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
// Roles that leave a token of theirs without every permission of the role it follows stop that token
// (said before, with its name), and taking
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
  // A token follows a role while its person holds every permission of it (a role they hold, or one their
  // roles cover, such as an admin's token following Agent): it stops when the new roles no longer cover it.
  const holdsAfter = new Set(roles.filter((role) => chosen.includes(role.id)).flatMap((role) => role.permissions));
  const holdsBefore = new Set(roles.filter((role) => member.roles.some((held) => held.id === role.id)).flatMap((role) => role.permissions));
  const stops = (role: Role) => role.permissions.every((name) => holdsBefore.has(name)) && !role.permissions.every((name) => holdsAfter.has(name));
  const tokensOf = (role: Role) => role.tokens.filter((token) => token.owner_id === member.id).map((token) => token.label);
  const stopping = roles.filter(stops).flatMap(tokensOf);
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
          const tokens = tokensOf(role);
          return stops(role) && tokens.length > 0 ? <p className="warning-note">{roleMessages.tokensStop(name, tokens)}</p> : null;
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

function RenameForm({ member, onCancel, onSave }: { member: TeamMember; onCancel: () => void; onSave: (name: string) => Promise<unknown> | void }) {
  const [failed, setFailed] = useState<string>();
  // The typed name is the page's unsaved change: the bar names it, it survives a tab switch, and leaving the page asks first.
  // The form stays open with the name and the server's words until the server took it.
  const [draft, setDraft] = useSettingsDraft(`rename:${member.id}`, member.display_name, { label: settingsMessages.teamRenameName(nameOf(member)), save: async (typed) => {
    setFailed(undefined);
    try { await onSave(typed.trim()); } catch (error) {
      setFailed(error instanceof InvalidRequestError || error instanceof PlanLimitError ? error.message : messages.settingsActionFailed);
      throw error;
    }
  } });
  const saveChanges = useSaveChanges();
  return (
    <form className="form-row team-rename" onSubmit={(event) => { event.preventDefault(); void saveChanges(); }}>
      {/* The field opens because the person asked for it, so it takes focus at once. */}
      <input aria-label={settingsMessages.teamDisplayNameOf(nameOf(member))} value={draft} maxLength={displayNameMaxLength} placeholder={member.email} autoFocus
        onChange={(event) => { setFailed(undefined); setDraft(event.target.value); }} onKeyDown={(event) => { if (event.key === 'Escape') { setDraft(member.display_name); onCancel(); } }} />
      <span className="button-row">
        <button type="button" onClick={() => { setDraft(member.display_name); onCancel(); }}>{settingsMessages.teamCancel}</button>
      </span>
      {failed && <p role="alert" className="error-copy">{failed}</p>}
    </form>
  );
}
