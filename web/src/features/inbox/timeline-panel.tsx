import type { RefObject } from 'react';
import type { Conversation, Message, TimeEntry } from '../../api/schemas';
import { messages } from '../../messages';
import { MessageItem } from './message-item';
import { PriorityLabel, StatusLabel } from './conversation-labels';
import { TimeEntriesSection } from './time-entries-section';

interface TimelinePanelProps {
  conversation?: Conversation;
  items: Message[];
  timeEntries?: TimeEntry[];
  timezone: string;
  onRetryDelivery?: (messageId: string) => void;
  detailsOpen?: boolean;
  onToggleDetails?: () => void;
  detailsToggleRef?: RefObject<HTMLButtonElement | null>;
}

export function TimelinePanel({ conversation, items, timeEntries = [], timezone, onRetryDelivery, detailsOpen = false, onToggleDetails, detailsToggleRef }: TimelinePanelProps) {
  return (
    <section className="timeline-panel" aria-label={messages.timeline}>
      <header className="timeline-header">
        <div>
          <h2>{conversation?.subject ?? messages.selectConversation}</h2>
          {conversation && <div className="timeline-meta"><StatusLabel status={conversation.status} /><PriorityLabel priority={conversation.priority} /></div>}
        </div>
        {conversation && onToggleDetails && <button type="button" className="details-toggle" ref={detailsToggleRef}
          aria-expanded={detailsOpen} aria-controls="conversation-details" onClick={onToggleDetails}>{messages.showDetails}</button>}
      </header>
      <div className="timeline-scroll">
        <div className="timeline-stack">
          {items.map((item) => <MessageItem key={item.id} item={item} timezone={timezone} onRetryDelivery={onRetryDelivery} />)}
          {timeEntries.length > 0 && <TimeEntriesSection items={timeEntries} timezone={timezone} />}
        </div>
      </div>
    </section>
  );
}
