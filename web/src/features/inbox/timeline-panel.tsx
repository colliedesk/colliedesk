import type { RefObject } from 'react';
import type { Conversation, ConversationViewer, Message, TimeEntry } from '../../api/schemas';
import { messages } from '../../messages';
import { Part } from '../../ui/lazy-part';
import { MessageItem } from './message-item';
import { presenceViewPart } from './presence-part';
import { PriorityLabel, StatusLabel } from './conversation-labels';
import { TimeEntriesSection } from './time-entries-section';

interface TimelinePanelProps {
  conversation?: Conversation;
  items: Message[];
  timeEntries?: TimeEntry[];
  loading?: boolean;
  // The ticket an address names could not be loaded: say so, and offer to try again.
  loadFailed?: boolean;
  onRetryLoad?: () => void;
  timezone: string;
  onRetryDelivery?: (messageId: string) => void;
  detailsOpen?: boolean;
  // An AI triage suggestion waits in the details, which a narrow screen hides behind the toggle.
  suggestionWaiting?: boolean;
  onToggleDetails?: () => void;
  detailsToggleRef?: RefObject<HTMLButtonElement | null>;
  // Team member names by id, to name the person who approved a reply an AI agent drafted.
  agentNames?: ReadonlyMap<string, string>;
  // Opens the customer's page: every conversation they had, newest first.
  onOpenContact?: (contactId: string) => void;
  // The others who have this conversation open now, and whether one is writing a reply.
  viewers?: ConversationViewer[];
}

export function TimelinePanel({ conversation, items, timeEntries = [], loading = false, loadFailed = false, onRetryLoad, timezone, onRetryDelivery, detailsOpen = false, suggestionWaiting = false, onToggleDetails, detailsToggleRef, agentNames, onOpenContact, viewers = [] }: TimelinePanelProps) {
  return (
    <section className="timeline-panel" aria-label={messages.timeline}>
      <header className="timeline-header">
        <div>
          <h2>{conversation?.subject ?? messages.selectConversation}</h2>
          {conversation && <div className="timeline-meta"><StatusLabel status={conversation.status} /><PriorityLabel priority={conversation.priority} />
            {/* A plain click opens the page in the app; a new tab or a copied address still works. */}
            <a className="timeline-contact" href={`/contacts/${conversation.contact_id}`} aria-label={messages.openContact(conversation.contact_name)}
              onClick={(event) => { if (onOpenContact && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && event.button === 0) { event.preventDefault(); onOpenContact(conversation.contact_id); } }}>
              {conversation.contact_name}</a></div>}
          {conversation && viewers.length > 0 && <Part part={presenceViewPart} render={(view) => <view.ConversationViewers viewers={viewers} timezone={timezone} />} />}
        </div>
        {conversation && onToggleDetails && <button type="button" className="details-toggle" ref={detailsToggleRef}
          aria-expanded={detailsOpen} aria-controls="conversation-details" onClick={onToggleDetails}>
          {messages.showDetails}{suggestionWaiting && <span className="details-badge"><span className="sr-only">, </span>{messages.aiSuggestionWaiting}</span>}</button>}
      </header>
      <div className="timeline-scroll">
        <div className="timeline-stack">
          {!conversation && loadFailed && <div role="alert" className="empty-copy"><p>{messages.conversationNotLoaded}</p>
            <button type="button" onClick={onRetryLoad}>{messages.retryLoad}</button></div>}
          {loading && items.length === 0 && <p role="status" className="empty-copy">{messages.loading}</p>}
          {items.map((item) => <MessageItem key={item.id} item={item} timezone={timezone} onRetryDelivery={onRetryDelivery}
            approvedBy={item.author_agent_id ? agentNames?.get(item.author_agent_id) : undefined} />)}
          {timeEntries.length > 0 && <TimeEntriesSection items={timeEntries} timezone={timezone} />}
        </div>
      </div>
    </section>
  );
}
