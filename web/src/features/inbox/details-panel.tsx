import { useState } from 'react';
import type { UpdateConversationRequest } from '../../api/client';
import type { TeamMember, Conversation, ConversationOrigin, Tag } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { AITriageSlot } from './ai-triage-slot';

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
}

export function DetailsPanel({ conversation, agents, tags, onUpdate, onAddTag, onRemoveTag, onCreateTag, timezone = 'UTC', onClose, onShowContactConversations, aiTriage = false, onTriageApplied, currentAgentId, canManage = false, onTriageWaiting }: DetailsPanelProps) {
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
      {aiTriage && conversation && <AITriageSlot conversationId={conversation.id} tags={tags} agents={agents} onApplied={() => onTriageApplied?.()}
        currentAgentId={currentAgentId} timezone={timezone} canManage={canManage} onWaiting={onTriageWaiting} />}
      <section className="details-section">
        <h2>{messages.properties}</h2>
        <label className="property-row"><span>{messages.status}</span><select value={conversation?.status ?? 'open'} disabled={!conversation}
          onChange={(event) => conversation && onUpdate?.(conversation.id, { status: event.target.value as UpdateConversationRequest['status'] })}>
          {Object.entries(messages.statuses).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
        </select></label>
        <label className="property-row"><span>{messages.priority}</span><select value={conversation?.priority ?? 'normal'} disabled={!conversation}
          onChange={(event) => conversation && onUpdate?.(conversation.id, { priority: event.target.value as UpdateConversationRequest['priority'] })}>
          {Object.entries(messages.priorities).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
        </select></label>
        <label className="property-row"><span>{messages.assignee}</span><select value={conversation?.assignee_id ?? ''} disabled={!conversation}
          onChange={(event) => conversation && onUpdate?.(conversation.id, { assignee_id: event.target.value || null })}>
          <option value="">{messages.unassigned}</option>
          {agents.filter((agent) => !agent.deactivated_at || agent.id === conversation?.assignee_id).map((agent) =>
            <option key={agent.id} value={agent.id}>{agent.display_name || agent.email}{agent.deactivated_at ? ` ${messages.deactivatedSuffix}` : ''}</option>)}
        </select></label>
      </section>
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
