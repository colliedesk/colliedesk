import Markdown from 'react-markdown';
import type { Attachment, Conversation, Message } from '../../api/schemas';
import { formatBytes } from '../../lib/format-bytes';
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
            <div className="message-meta"><span><strong>{item.kind === 'note' ? messages.noteMode : item.kind === 'incoming' ? messages.incoming : messages.replyMode}</strong>
              {item.author_name && <span className="message-author">{item.author_name}</span>}</span>
              <time dateTime={item.created_at}>{formatDate(item.created_at, timezone)}</time></div>
            <div className="markdown-body"><Markdown>{item.body}</Markdown></div>
            {item.attachments.length > 0 && <AttachmentList items={item.attachments} />}
          </article>
        ))}
      </div>
    </section>
  );
}

function AttachmentList({ items }: { items: Attachment[] }) {
  return (
    <ul className="message-attachments" aria-label={messages.attachments}>
      {items.map((file) => (
        <li key={file.id}>
          <a href={`/api/v1/attachments/${file.id}`} download={file.filename} title={messages.downloadAttachment(file.filename)}>{file.filename}</a>
          <span className="attachment-size">{formatBytes(file.size)}</span>
        </li>
      ))}
    </ul>
  );
}
