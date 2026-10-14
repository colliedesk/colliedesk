import type { Conversation, Message, TimeEntry } from '../../api/schemas';
import { messages } from '../../messages';
import { MessageItem } from './message-item';
import { TimeEntriesSection } from './time-entries-section';

interface TimelinePanelProps {
  conversation?: Conversation;
  items: Message[];
  timeEntries?: TimeEntry[];
  timezone: string;
}

export function TimelinePanel({ conversation, items, timeEntries = [], timezone }: TimelinePanelProps) {
  return (
    <section className="timeline-panel" aria-label={messages.timeline}>
      <header className="timeline-header">
        <div><h2>{conversation?.subject ?? messages.selectConversation}</h2>
          {conversation && <span className={`status status-${conversation.status}`}>{messages.statuses[conversation.status]}</span>}
        </div>
      </header>
      <div className="timeline-scroll">
        {items.map((item) => <MessageItem key={item.id} item={item} timezone={timezone} />)}
        {timeEntries.length > 0 && <TimeEntriesSection items={timeEntries} timezone={timezone} />}
      </div>
    </section>
  );
}
