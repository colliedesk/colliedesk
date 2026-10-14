import Markdown from 'react-markdown';
import type { Conversation, Message } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';

interface TimelinePanelProps {
  conversation?: Conversation;
  items: Message[];
  timezone: string;
}

export function TimelinePanel({ conversation, items, timezone }: TimelinePanelProps) {
  return (
    <section className="timeline-panel" aria-label={messages.timeline}>
      <header className="timeline-header">
        <div><h2>{conversation?.subject ?? messages.selectConversation}</h2>
          {conversation && <span className={`status status-${conversation.status}`}>{messages.statuses[conversation.status]}</span>}
        </div>
      </header>
      <div className="timeline-scroll">
        {items.map((item) => (
          <article className={`message message-${item.kind}`} key={item.id}>
            <div className="message-meta"><strong>{item.kind === 'note' ? messages.noteMode : item.kind === 'incoming' ? messages.incoming : messages.replyMode}</strong>
              <time dateTime={item.created_at}>{formatDate(item.created_at, timezone)}</time></div>
            <div className="markdown-body"><Markdown>{item.body}</Markdown></div>
          </article>
        ))}
      </div>
    </section>
  );
}
