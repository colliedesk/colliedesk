import { useState } from 'react';
import type { UpdateConversationRequest } from '../../api/client';
import type { Agent, Conversation, ConversationOrigin, Tag } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';

interface DetailsPanelProps {
  conversation?: Conversation;
  agents: Agent[];
  tags: Tag[];
  onUpdate?: (conversationId: string, value: UpdateConversationRequest) => void;
  onAddTag?: (conversationId: string, tagId: string) => void;
  onRemoveTag?: (conversationId: string, tagId: string) => void;
  onCreateTag?: (conversationId: string, name: string) => void;
  timezone?: string;
  onClose?: () => void;
}

export function DetailsPanel({ conversation, agents, tags, onUpdate, onAddTag, onRemoveTag, onCreateTag, timezone = 'UTC', onClose }: DetailsPanelProps) {
  const [tagName, setTagName] = useState('');
  const createTag = () => {
    if (!conversation || !tagName.trim()) return;
    onCreateTag?.(conversation.id, tagName.trim());
    setTagName('');
  };
  return (
    <aside id="conversation-details" className="details-panel" aria-label={messages.conversationDetails}
      onKeyDown={(event) => { if (event.key === 'Escape' && onClose) onClose(); }}>
      {onClose && <button type="button" className="drawer-close" onClick={onClose} autoFocus>{messages.hideDetails}</button>}
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
          {agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.display_name || agent.email}</option>)}
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
