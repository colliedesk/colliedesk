import { useId, useState } from 'react';
import type { ImportPerson } from '../../api/imports';
import type { Invite, TeamMember } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { importMessages as m } from './import-messages';

// CreatedInvite is the one-time link of an invite just made: the server shows it once.
// url is missing when a repeat of the press came back without the link, which is shown once.
export interface CreatedInvite { inviteId: string; email: string; url?: string; expiresAt: string }

export interface ImportPeopleProps {
  people: ImportPerson[];
  // agents is the team; only active agents can be linked.
  agents: TeamMember[];
  invites: Invite[];
  // createdInvites are the invites made on this page, with their one-time links.
  createdInvites?: CreatedInvite[];
  // imported is true once the import wrote replies a link moves; before that a link has nothing to move.
  imported: boolean;
  timezone: string;
  failed?: boolean;
  busy?: boolean;
  onRetry: () => void;
  onInvite: (email: string) => void;
  onRevokeInvite: (id: string) => void;
  onLink: (externalId: string, agentId: string) => void;
  onUnlink: (externalId: string) => void;
}

const agentName = (agent: TeamMember) => agent.display_name || agent.email;
const pendingInvite = (invite: Invite) => !invite.used_at && !invite.revoked_at && Date.parse(invite.expires_at) > Date.now();

// ImportPeople lists the agents of the Freshdesk account, so the team comes along with the tickets.
export function ImportPeople(props: ImportPeopleProps) {
  const { people, agents, invites, failed, busy, onRetry } = props;
  const active = agents.filter((agent) => !agent.deactivated_at);
  const invited = new Map(invites.filter(pendingInvite).map((invite) => [invite.email.toLowerCase(), invite]));
  const members = new Set(agents.map((agent) => agent.email.toLowerCase()));
  return <section className="settings-card" aria-labelledby="import-people-heading">
    <div className="section-heading"><h2 id="import-people-heading">{m.peopleHeading}</h2><p>{m.peopleHelp}</p></div>
    <div className="settings-body">
      {failed && <p role="alert" className="error-copy">{m.peopleFailed}{' '}
        <button type="button" className="link-action" onClick={onRetry}>{m.peopleRetry}</button></p>}
      {people.length > 0 && <ul className="import-people">
        {people.map((person) => <Person key={person.external_id} {...props} person={person} active={active}
          invite={invited.get(person.email.toLowerCase())} member={members.has(person.email.toLowerCase())} busy={busy} />)}
      </ul>}
    </div>
  </section>;
}

function Person({ person, active, invite, member, createdInvites = [], imported, timezone, busy, onInvite, onRevokeInvite, onLink, onUnlink }:
  ImportPeopleProps & { person: ImportPerson; active: TeamMember[]; invite?: Invite; member: boolean }) {
  const linked = active.find((agent) => agent.id === person.linked_agent_id);
  const email = person.email.toLowerCase();
  const justInvited = createdInvites.find((link) => link.email.toLowerCase() === email && (!invite || link.inviteId === invite.id));
  const state = linked ? m.personLinked(agentName(linked)) : person.pending && imported ? m.personLinking : invite ? m.personInvited : m.personNotLinked;
  return <li>
    <div className="import-person">
      <span className="import-person-name">{person.name}</span>
      <span className="muted-copy">{person.email}{person.active ? '' : ` · ${m.personInactive}`}</span>
    </div>
    <span className="import-person-state" role="status" data-linked={linked ? true : undefined}>
      {state}{linked && person.pending && imported && <small className="muted-copy"> · {m.movingReplies}</small>}
    </span>
    {justInvited && <InviteLink invite={justInvited} timezone={timezone} />}
    <div className="button-row">
      {linked ? <button type="button" aria-label={m.unlinkLabel(person.name)} onClick={() => onUnlink(person.external_id)} disabled={busy}>{m.unlink}</button>
        : <LinkTo person={person} agents={active} busy={busy} onLink={onLink} />}
      {!linked && !member && !invite && email.includes('@')
        && <button type="button" aria-label={m.inviteLabel(person.name)} onClick={() => onInvite(person.email)} disabled={busy}>{m.invite}</button>}
      {!linked && invite && <button type="button" aria-label={m.revokeInviteLabel(person.name)} onClick={() => onRevokeInvite(invite.id)} disabled={busy}>{m.revokeInvite}</button>}
    </div>
  </li>;
}

function InviteLink({ invite, timezone }: { invite: CreatedInvite; timezone: string }) {
  const [copied, setCopied] = useState<'yes' | 'failed'>();
  const id = useId();
  const url = invite.url;
  if (!url) return <p role="status" className="import-invite-link">{messages.inviteLinkShownOnce}</p>;
  return <div className="import-invite-link">
    <label htmlFor={id} className="field-label">{messages.inviteLink}</label>
    <div className="button-row">
      <input id={id} readOnly value={url} onFocus={(event) => event.target.select()} />
      <button type="button" onClick={() => {
        const write = navigator.clipboard?.writeText(url) ?? Promise.reject(new Error('no clipboard'));
        void write.then(() => setCopied('yes'), () => setCopied('failed'));
      }}>{copied === 'yes' ? m.copied : m.copyLink}</button>
    </div>
    {copied === 'failed' && <small role="alert" className="error-copy">{m.copyFailed}</small>}
    <small className="muted-copy">{m.inviteExpires(formatDate(invite.expiresAt, timezone))}</small>
  </div>;
}

function LinkTo({ person, agents, busy, onLink }: { person: ImportPerson; agents: TeamMember[]; busy?: boolean; onLink: ImportPeopleProps['onLink'] }) {
  const [agentId, setAgentId] = useState('');
  const id = useId();
  if (agents.length === 0) return null;
  return <>
    <label htmlFor={id} className="sr-only">{m.linkTo(person.name)}</label>
    <select id={id} value={agentId} onChange={(event) => setAgentId(event.target.value)} disabled={busy}>
      <option value="">{m.chooseAgent}</option>
      {agents.map((agent) => <option key={agent.id} value={agent.id}>{agentName(agent)}</option>)}
    </select>
    <button type="button" aria-label={m.linkLabel(person.name)} onClick={() => onLink(person.external_id, agentId)} disabled={busy || !agentId}>{m.link}</button>
  </>;
}
