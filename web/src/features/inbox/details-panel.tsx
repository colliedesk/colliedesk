import { useId, useState } from 'react';
import type { UpdateConversationRequest } from '../../api/client';
import type { TeamMember, Conversation, ConversationOrigin, Tag } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { clockLine, slaOf } from '../../lib/sla';
import { messages } from '../../messages';
import { CheckIcon } from '../../ui/icons';
import { ActionMenu } from './action-menu';
import { AITriageSlot } from './ai-triage-slot';
import { StatusLabel } from './conversation-labels';
import { mayTell, noticesOf } from './status-tell';
import { AutofixDetails } from '../autofix/autofix-slot';

interface DetailsPanelProps {
  conversation?: Conversation;
  agents: TeamMember[];
  tags: Tag[];
  onUpdate?: (conversationId: string, value: UpdateConversationRequest) => void;
  onAddTag?: (conversationId: string, tagId: string) => void;
  onRemoveTag?: (conversationId: string, tagId: string) => void;
  onCreateTag?: (conversationId: string, name: string) => void;
  timezone?: string;
  onClose?: () => void;
  // Narrows the list to this customer's conversations.
  onShowContactConversations?: (contactId: string) => void;
  // Triage suggestions are on in the workspace; onTriageApplied reads the conversation again after a part is applied.
  aiTriage?: boolean;
  onTriageApplied?: () => void;
  // The signed-in agent and whether they are an admin, for the suggestion's wording and next steps.
  currentAgentId?: string;
  canManage?: boolean;
  // Told whether the suggestion has a part nobody accepted, for the toggle that hides the panel.
  onTriageWaiting?: (waiting: boolean) => void;
  // The server runs Autofix: the details open with its block.
  autofix?: boolean;
}

const statusOrder = ['pending', 'resolved', 'closed', 'open'] as const;

// Status is a menu, not a select: a closed select fires change on every arrow key in some browsers, and each
// step would change the conversation (and on a chat tell the customer). Where a status may tell, Tell the
// customer comes first, ticked each time the menu opens.
function StatusPick({ conversation, onUpdate }: { conversation?: Conversation; onUpdate?: DetailsPanelProps['onUpdate'] }) {
  const [tell, setTell] = useState(true);
  const id = useId();
  const status = conversation?.status ?? 'open';
  // Asked only where a status may tell: a chat whose channel is on and has words (the server's word).
  const asks = conversation ? mayTell(conversation) : false;
  const channel = conversation?.channel_ref?.name ?? messages.theChannel;
  const others = statusOrder.filter((value) => value !== status);
  const notices = conversation ? noticesOf(conversation) : [];
  const told = others.filter((value) => notices?.includes(value));
  const quiet = others.filter((value) => !told.includes(value));
  return <div className="property-row status-pick-row"><span id={`${id}-label`}>{messages.status}</span>
    <ActionMenu<Conversation['status']> className="status-pick" label={messages.status} labelledBy={`${id}-label ${id}-value`} disabled={!conversation || !onUpdate}
      summary={<span id={`${id}-value`}><StatusLabel status={status} /></span>}
      onToggle={(open) => { if (open) setTell(true); }}
      onApply={(value) => conversation && onUpdate?.(conversation.id, !asks || tell ? { status: value } : { status: value, notify: false })}>
      {(apply) => <>
        {asks && <>
          <label className="field-inline status-pick-tell"><input type="checkbox" checked={tell} onChange={(event) => setTell(event.target.checked)} /><span>{messages.tellCustomer}</span></label>
          <small className="status-pick-which">{!tell ? messages.tellUnticked : notices === undefined ? messages.tellUnknown(channel)
            : told.length === 0 ? messages.tellNone(channel) : messages.tellWhich(channel, told.map((value) => messages.statusChange[value]), quiet.map((value) => messages.statusChange[value]))}</small>
        </>}
        {(['open', 'pending', 'resolved', 'closed'] as const).filter((value) => value !== status)
          .map((value) => <button type="button" key={value} onClick={() => apply(value)}><StatusLabel status={value} /></button>)}
      </>}
    </ActionMenu></div>;
}

export function DetailsPanel({ conversation, agents, tags, onUpdate, onAddTag, onRemoveTag, onCreateTag, timezone = 'UTC', onClose, onShowContactConversations, aiTriage = false, onTriageApplied, currentAgentId, canManage = false, onTriageWaiting, autofix = false }: DetailsPanelProps) {
  const [tagName, setTagName] = useState('');
  const createTag = () => {
    if (!conversation || !tagName.trim()) return;
    onCreateTag?.(conversation.id, tagName.trim());
    setTagName('');
  };
  return (
    <aside id="conversation-details" className="details-panel panel" aria-label={messages.conversationDetails}
      onKeyDown={(event) => { if (event.key === 'Escape' && onClose && !(event.target instanceof HTMLSelectElement)) onClose(); }}>
      {onClose && <button type="button" className="drawer-close" onClick={onClose} autoFocus>{messages.hideDetails}</button>}
      {autofix && conversation && <AutofixDetails conversationId={conversation.id} customerName={conversation.contact_name} timezone={timezone} />}
      {aiTriage && conversation && <AITriageSlot conversationId={conversation.id} tags={tags} agents={agents} onApplied={() => onTriageApplied?.()}
        currentAgentId={currentAgentId} timezone={timezone} canManage={canManage} onWaiting={onTriageWaiting} />}
      <section className="details-section">
        <h2>{messages.properties}</h2>
        <StatusPick key={conversation?.id} conversation={conversation} onUpdate={onUpdate} />
        <label className="property-row"><span>{messages.priority}</span><select value={conversation?.priority ?? 'normal'} disabled={!conversation}
          onChange={(event) => conversation && onUpdate?.(conversation.id, { priority: event.target.value as UpdateConversationRequest['priority'] })}>
          {Object.entries(messages.priorities).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
        </select></label>
        <label className="property-row"><span>{messages.assignee}</span><select value={conversation?.assignee_id ?? ''} disabled={!conversation}
          onChange={(event) => conversation && onUpdate?.(conversation.id, { assignee_id: event.target.value || null })}>
          {/* The signed-in agent is Me, first; teammates follow and Unassigned comes last, as in the Filter menu. */}
          {currentAgentId && <option value={currentAgentId}>{messages.assignToMe}</option>}
          {agents.filter((agent) => agent.id !== currentAgentId && (!agent.deactivated_at || agent.id === conversation?.assignee_id)).map((agent) =>
            <option key={agent.id} value={agent.id}>{agent.display_name || agent.email}{agent.deactivated_at ? ` ${messages.deactivatedSuffix}` : ''}</option>)}
          <option value="">{messages.unassigned}</option>
        </select></label>
      </section>
      {conversation && slaOf(conversation) && <Targets sla={slaOf(conversation)!} timezone={timezone} canManage={canManage} />}
      <fieldset className="details-section" disabled={!conversation}><legend>{messages.tags}</legend>
        <div className="tag-choices">
          {tags.length === 0 && <p className="muted-copy">{messages.noTagsYet}</p>}
          {tags.map((tag) => {
            const checked = conversation?.tags.some((item) => item.id === tag.id) ?? false;
            return <label className="tag-check" key={tag.id}><input type="checkbox" checked={checked}
              onChange={() => conversation && (checked ? onRemoveTag?.(conversation.id, tag.id) : onAddTag?.(conversation.id, tag.id))} />{tag.name}</label>;
          })}
        </div>
        <form className="tag-create" onSubmit={(event) => { event.preventDefault(); createTag(); }}>
          <input aria-label={messages.newTagName} placeholder={messages.newTagName} value={tagName} onChange={(event) => setTagName(event.target.value)} />
          <button type="submit" disabled={!conversation || !tagName.trim()}>{messages.createTag}</button>
        </form>
      </fieldset>
      {conversation && (
        <section className="details-section conversation-source">
          {onShowContactConversations && <button type="button" className="link-button" onClick={() => onShowContactConversations(conversation.contact_id)}>
            {messages.showContactConversations(conversation.contact_name)}</button>}
        </section>
      )}
      {conversation?.origin && <ConversationOriginBlock origin={conversation.origin} timezone={timezone} />}
    </aside>
  );
}

function ConversationOriginBlock({ origin, timezone }: { origin: ConversationOrigin; timezone: string }) {
  const system = messages.systemNames[origin.system] ?? origin.system;
  return (
    <section className="details-section conversation-origin">
      <span>{messages.importedTicket(system, origin.external_id)}</span>
      {origin.type && <span>{messages.ticketType(origin.type)}</span>}
      {origin.due_at && <span>{messages.dueAt} <time dateTime={origin.due_at}>{formatDate(origin.due_at, timezone)}</time></span>}
    </section>
  );
}

// Targets: each clock in full, read only. The server counts business hours; the details say what it counted.
function Targets({ sla, timezone, canManage }: { sla: NonNullable<ReturnType<typeof slaOf>>; timezone: string; canManage: boolean }) {
  const rows: [string, ReturnType<typeof clockLine>][] = [[messages.targetFirstReply, clockLine(sla.first_response, timezone)], [messages.targetResolve, clockLine(sla.resolution, timezone)]];
  return <section className="details-section" aria-labelledby="details-targets">
    <h2 id="details-targets">{messages.targets}</h2>
    {rows.map(([label, line]) => <div className="target-row" key={label}><span>{label}</span>
      <span className={`target-value${line.tone ? ` target-${line.tone}` : ''}`}><span>{line.tone === 'met' && <CheckIcon />}{line.text}</span>{line.detail && <small>{line.detail}</small>}</span></div>)}
    <p className="targets-foot">{messages.targetsFoot}{canManage && <> <a href="/settings/targets">{messages.editTargets}</a></>}</p>
  </section>;
}
