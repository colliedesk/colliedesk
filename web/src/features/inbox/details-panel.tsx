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
}

export function DetailsPanel({ conversation, agents, tags, onUpdate, onAddTag, onRemoveTag, onCreateTag, timezone = 'UTC' }: DetailsPanelProps) {
  const [tagName, setTagName] = useState('');
  return (
    <aside className="details-panel" aria-label={messages.conversationDetails}>
      <h2>{messages.conversationDetails}</h2>
      {conversation?.origin && <ConversationOriginBlock origin={conversation.origin} timezone={timezone} />}
      <label><span>{messages.status}</span><select value={conversation?.status ?? 'open'} disabled={!conversation}
        onChange={(event) => conversation && onUpdate?.(conversation.id, { status: event.target.value as UpdateConversationRequest['status'] })}>
        {Object.entries(messages.statuses).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select></label>
      <label><span>{messages.priority}</span><select value={conversation?.priority ?? 'normal'} disabled={!conversation}
        onChange={(event) => conversation && onUpdate?.(conversation.id, { priority: event.target.value as UpdateConversationRequest['priority'] })}>
        {Object.entries(messages.priorities).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select></label>
      <label><span>{messages.assignee}</span><select value={conversation?.assignee_id ?? ''} disabled={!conversation}
        onChange={(event) => conversation && onUpdate?.(conversation.id, { assignee_id: event.target.value || null })}>
        <option value="">{messages.unassigned}</option>
        {agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.display_name || agent.email}</option>)}
      </select></label>
      <fieldset disabled={!conversation}><legend>{messages.tags}</legend>
        {tags.map((tag) => {
          const checked = conversation?.tags.some((item) => item.id === tag.id) ?? false;
          return <label className="tag-check" key={tag.id}><input type="checkbox" checked={checked}
            onChange={() => conversation && (checked ? onRemoveTag?.(conversation.id, tag.id) : onAddTag?.(conversation.id, tag.id))} />{tag.name}</label>;
        })}
        <div className="tag-create"><input aria-label={messages.newTagName} value={tagName}
          onChange={(event) => setTagName(event.target.value)} />
          <button type="button" disabled={!conversation || !tagName.trim()} onClick={() => {
            if (conversation) onCreateTag?.(conversation.id, tagName.trim()); setTagName('');
          }}>{messages.createTag}</button></div>
      </fieldset>
    </aside>
  );
}

function ConversationOriginBlock({ origin, timezone }: { origin: ConversationOrigin; timezone: string }) {
  const system = messages.systemNames[origin.system] ?? origin.system;
  return (
    <div className="conversation-origin">
      <span>{messages.importedTicket(system, origin.external_id)}</span>
      {origin.type && <span>{messages.ticketType(origin.type)}</span>}
      {origin.due_at && <span>{messages.dueAt} <time dateTime={origin.due_at}>{formatDate(origin.due_at, timezone)}</time></span>}
    </div>
  );
}
