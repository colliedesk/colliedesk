import { useEffect, useRef, useState } from 'react';
import type { TeamMember } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';
import { displayNameMaxLength } from './display-name-field';

interface TeamSectionProps {
  members: TeamMember[];
  currentAgentId?: string;
  canManage: boolean;
  timezone: string;
  error?: string;
  onChangeRole?: (id: string, role: 'admin' | 'agent') => void;
  // name is trimmed; an empty name clears it and the email is shown again.
  onRename?: (id: string, name: string) => void;
  // reassignTo is the agent that takes the open and pending conversations, or null for Unassigned.
  onDeactivate?: (id: string, reassignTo: string | null) => void;
  onReactivate?: (id: string) => void;
}

const nameOf = (member: TeamMember) => member.display_name || member.email;

export function TeamSection({ members, currentAgentId, canManage, timezone, error, onChangeRole, onRename, onDeactivate, onReactivate }: TeamSectionProps) {
  const [confirming, setConfirming] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  // roleDraft holds a chosen but not yet applied role; nothing changes until Apply is pressed.
  const [roleDraft, setRoleDraft] = useState<Record<string, 'admin' | 'agent'>>({});
  // After a question or the rename form closes, focus goes to the row (done) or the button that opened it (cancelled).
  const [refocus, setRefocus] = useState<{ id: string; target: 'row' | 'button' | 'rename' } | null>(null);
  const rows = useRef(new Map<string, HTMLElement>());
  const buttons = useRef(new Map<string, HTMLElement>());
  const renameButtons = useRef(new Map<string, HTMLElement>());
  useEffect(() => {
    if (!refocus) return;
    ({ row: rows, button: buttons, rename: renameButtons })[refocus.target].current.get(refocus.id)?.focus();
    setRefocus(null);
  }, [refocus]);
  // The server refuses to leave a workspace without an active admin; the button is not offered either.
  const activeAdmins = members.filter((member) => member.role === 'admin' && member.deactivated_at === null).length;
  return (
    <section className="settings-card" aria-labelledby="team-heading">
      <div className="section-heading"><h2 id="team-heading">{messages.team}</h2><p>{settingsMessages.teamDescription}</p></div>
      <div className="settings-body">
        {error && <p role="alert" className="error-copy">{error}</p>}
        <div className="data-list team-list">{members.map((member) => {
          const name = nameOf(member);
          const deactivated = member.deactivated_at !== null;
          const draft = roleDraft[member.id] ?? member.role;
          const lastAdmin = member.role === 'admin' && !deactivated && activeAdmins === 1;
          return <article key={member.id} tabIndex={-1} ref={(node) => { if (node) rows.current.set(member.id, node); else rows.current.delete(member.id); }}>
            <div className="team-cells">
              <span className="team-person"><strong title={member.email}>{name}{member.id === currentAgentId && <small> ({settingsMessages.teamYou})</small>}</strong>
                {member.display_name && <small>{member.email}</small>}</span>
              {canManage && !deactivated
                ? <span className="team-role">
                    <select aria-label={settingsMessages.teamRoleOf(name)} value={draft} disabled={lastAdmin} title={lastAdmin ? settingsMessages.teamLastAdmin : undefined} onChange={(event) => setRoleDraft({ ...roleDraft, [member.id]: event.target.value as 'admin' | 'agent' })}>
                      <option value="agent">{messages.roles.agent}</option><option value="admin">{messages.roles.admin}</option>
                    </select>
                    {draft !== member.role && <button type="button" aria-label={settingsMessages.teamApplyRoleFor(name)}
                      onClick={() => { onChangeRole?.(member.id, draft); setRoleDraft(({ [member.id]: _applied, ...rest }) => rest); }}>{settingsMessages.teamApplyRole}</button>}
                  </span>
                : <span>{messages.roles[member.role]}</span>}
              <span>{deactivated ? settingsMessages.teamDeactivated : settingsMessages.teamActive}</span>
              <time>{member.last_active_at ? settingsMessages.teamLastActive(formatDate(member.last_active_at, timezone)) : settingsMessages.teamNeverSignedIn}</time>
            </div>
            {canManage && <span className="team-actions">
              {renaming !== member.id &&
                <button type="button" aria-label={settingsMessages.teamRenameName(name)} ref={(node) => { if (node) renameButtons.current.set(member.id, node); else renameButtons.current.delete(member.id); }}
                  onClick={() => { setConfirming(null); setRenaming(member.id); }}>{settingsMessages.teamRename}</button>}
              {!deactivated && !lastAdmin && confirming !== member.id &&
                <button type="button" className="danger-action" aria-label={settingsMessages.teamDeactivateName(name)} ref={(node) => { if (node) buttons.current.set(member.id, node); else buttons.current.delete(member.id); }} onClick={() => { setRenaming(null); setConfirming(member.id); }}>{settingsMessages.teamDeactivate}</button>}
              {deactivated &&
                <button type="button" aria-label={settingsMessages.teamReactivateName(name)} title={settingsMessages.teamReactivateHelp} onClick={() => onReactivate?.(member.id)}>{settingsMessages.teamReactivate}</button>}
            </span>}
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
