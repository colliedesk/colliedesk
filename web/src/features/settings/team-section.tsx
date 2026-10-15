import { useEffect, useRef, useState } from 'react';
import type { TeamMember } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';

interface TeamSectionProps {
  members: TeamMember[];
  currentAgentId?: string;
  canManage: boolean;
  timezone: string;
  error?: string;
  onChangeRole?: (id: string, role: 'admin' | 'agent') => void;
  // reassignTo is the agent that takes the open and pending conversations, or null for Unassigned.
  onDeactivate?: (id: string, reassignTo: string | null) => void;
  onReactivate?: (id: string) => void;
}

const nameOf = (member: TeamMember) => member.display_name || member.email;

export function TeamSection({ members, currentAgentId, canManage, timezone, error, onChangeRole, onDeactivate, onReactivate }: TeamSectionProps) {
  const [confirming, setConfirming] = useState<string | null>(null);
  // roleDraft holds a chosen but not yet applied role; nothing changes until Apply is pressed.
  const [roleDraft, setRoleDraft] = useState<Record<string, 'admin' | 'agent'>>({});
  // After the question closes, focus goes to the row (confirmed) or its Deactivate button (cancelled).
  const [refocus, setRefocus] = useState<{ id: string; target: 'row' | 'button' } | null>(null);
  const rows = useRef(new Map<string, HTMLElement>());
  const buttons = useRef(new Map<string, HTMLElement>());
  useEffect(() => {
    if (!refocus) return;
    (refocus.target === 'row' ? rows : buttons).current.get(refocus.id)?.focus();
    setRefocus(null);
  }, [refocus]);
  return (
    <section className="settings-card" aria-labelledby="team-heading">
      <div className="section-heading"><h2 id="team-heading">{messages.team}</h2><p>{messages.teamDescription}</p></div>
      <div className="settings-body">
        {error && <p role="alert" className="error-copy">{error}</p>}
        <div className="data-list team-list">{members.map((member) => {
          const name = nameOf(member);
          const deactivated = member.deactivated_at !== null;
          const draft = roleDraft[member.id] ?? member.role;
          return <article key={member.id} tabIndex={-1} ref={(node) => { if (node) rows.current.set(member.id, node); else rows.current.delete(member.id); }}>
            <div>
              <span className="team-person"><strong title={member.email}>{name}{member.id === currentAgentId && <small> ({messages.teamYou})</small>}</strong>
                {member.display_name && <small>{member.email}</small>}</span>
              {canManage && !deactivated
                ? <span className="team-role">
                    <select aria-label={messages.teamRoleOf(name)} value={draft} onChange={(event) => setRoleDraft({ ...roleDraft, [member.id]: event.target.value as 'admin' | 'agent' })}>
                      <option value="agent">{messages.roles.agent}</option><option value="admin">{messages.roles.admin}</option>
                    </select>
                    {draft !== member.role && <button type="button" aria-label={messages.teamApplyRoleFor(name)}
                      onClick={() => { onChangeRole?.(member.id, draft); setRoleDraft(({ [member.id]: _applied, ...rest }) => rest); }}>{messages.teamApplyRole}</button>}
                  </span>
                : <span>{messages.roles[member.role]}</span>}
              <span>{deactivated ? messages.teamDeactivated : messages.teamActive}</span>
              <time>{member.last_active_at ? messages.teamLastActive(formatDate(member.last_active_at, timezone)) : messages.teamNeverSignedIn}</time>
            </div>
            {canManage && !deactivated && confirming !== member.id &&
              <button type="button" className="danger-action" aria-label={messages.teamDeactivateName(name)} ref={(node) => { if (node) buttons.current.set(member.id, node); else buttons.current.delete(member.id); }} onClick={() => setConfirming(member.id)}>{messages.teamDeactivate}</button>}
            {canManage && deactivated &&
              <button type="button" aria-label={messages.teamReactivateName(name)} title={messages.teamReactivateHelp} onClick={() => onReactivate?.(member.id)}>{messages.teamReactivate}</button>}
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
      <strong id={headingId}>{messages.teamDeactivateQuestion(name)}</strong>
      <p>{messages.teamOpenWork(member.open_conversations)} {messages.teamKeepsHistory}{isSelf && ` ${messages.teamSignedOut}`}</p>
      {hasWork && <>
        <label><input type="radio" name={`reassign-${member.id}`} checked={!assign} onChange={() => setAssign(false)} /> {messages.teamMoveToUnassigned}</label>
        <label><input type="radio" name={`reassign-${member.id}`} checked={assign} disabled={others.length === 0} onChange={() => setAssign(true)} /> {messages.teamAssignToAnother}</label>
        {assign && <select aria-label={messages.teamAssignTo} value={target} onChange={(event) => setTarget(event.target.value)}>
          {others.map((other) => <option key={other.id} value={other.id}>{nameOf(other)}</option>)}
        </select>}
      </>}
      <span className="button-row">
        <button type="button" className="danger-action" onClick={() => onConfirm(hasWork && assign ? target : null)}>{messages.teamConfirmDeactivate}</button>
        <button type="button" onClick={onCancel}>{messages.teamCancel}</button>
      </span>
    </div>
  );
}
