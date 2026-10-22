import { useId, useState } from 'react';
import type { ImportPerson } from '../../api/imports';
import type { Invite, TeamMember } from '../../api/schemas';
import { importMessages as m } from './import-messages';

export interface ImportPeopleProps {
  people: ImportPerson[];
  // agents is the team; only active agents can be linked.
  agents: TeamMember[];
  invites: Invite[];
  failed?: boolean;
  busy?: boolean;
  onInvite: (email: string) => void;
  onLink: (externalId: string, agentId: string) => void;
  onUnlink: (externalId: string) => void;
}

const agentName = (agent: TeamMember) => agent.display_name || agent.email;

// ImportPeople lists the agents of the Freshdesk account, so the team comes along with the tickets.
export function ImportPeople({ people, agents, invites, failed, busy, onInvite, onLink, onUnlink }: ImportPeopleProps) {
  const active = agents.filter((agent) => !agent.deactivated_at);
  const invited = new Set(invites.filter((invite) => !invite.used_at && !invite.revoked_at && Date.parse(invite.expires_at) > Date.now()).map((invite) => invite.email.toLowerCase()));
  const members = new Set(agents.map((agent) => agent.email.toLowerCase()));
  return <section className="settings-card" aria-labelledby="import-people-heading">
    <div className="section-heading"><h2 id="import-people-heading">{m.peopleHeading}</h2><p>{m.peopleHelp}</p></div>
    <div className="settings-body">
      {failed && <p role="alert" className="error-copy">{m.peopleFailed}</p>}
      <ul className="import-people">
        {people.map((person) => {
          const linked = active.find((agent) => agent.id === person.linked_agent_id);
          const email = person.email.toLowerCase();
          return <li key={person.external_id}>
            <div className="import-person">
              <span className="import-person-name">{person.name}</span>
              <span className="muted-copy">{person.email}{person.active ? '' : ` · ${m.personInactive}`}</span>
            </div>
            <span className="import-person-state" data-linked={linked ? true : undefined}>
              {person.pending ? m.personLinking : linked ? m.personLinked(agentName(linked)) : invited.has(email) ? m.personInvited : m.personNotLinked}
            </span>
            <div className="button-row">
              {linked ? <button type="button" onClick={() => onUnlink(person.external_id)} disabled={busy}>{m.unlink}</button>
                : <LinkTo person={person} agents={active} busy={busy} onLink={onLink} />}
              {!linked && !members.has(email) && !invited.has(email) && email.includes('@')
                && <button type="button" onClick={() => onInvite(person.email)} disabled={busy}>{m.invite}</button>}
            </div>
          </li>;
        })}
      </ul>
    </div>
  </section>;
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
    <button type="button" onClick={() => onLink(person.external_id, agentId)} disabled={busy || !agentId}>{m.link}</button>
  </>;
}
