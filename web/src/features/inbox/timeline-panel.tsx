import Markdown from 'react-markdown';
import type { Conversation, Message } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { PlainTextBody } from './plain-text-body';

interface TimelinePanelProps {
  conversation?: Conversation;
  items: Message[];
  timezone: string;
  onRetryDelivery?: (messageId: string) => void;
}

function kindLabel(kind: Message['kind']) {
  if (kind === 'note') return messages.noteMode;
  return kind === 'incoming' ? messages.incoming : messages.replyMode;
}

function EmailEnvelope({ email }: { email: NonNullable<Message['email']> }) {
  const warning = email.authentication === 'fail' ? messages.senderFailedVerification
    : email.authentication === 'none' ? messages.senderNotVerified : undefined;
  return (
    <div className="message-envelope">
      <span>{email.from}</span>
      {warning && <span className={`badge badge-${email.authentication}`}>{warning}</span>}
      {email.attachments.length > 0 && <span>{messages.attachmentsNotImported(email.attachments.map((item) => item.filename))}</span>}
    </div>
  );
}

function DeliveryStatus({ item, onRetry }: { item: Message; onRetry?: (messageId: string) => void }) {
  if (!item.delivery) return null;
  const { status, last_error: lastError } = item.delivery;
  return (
    <div className="message-envelope">
      <span className={`badge badge-delivery-${status}`} title={lastError ?? undefined}>{messages.deliveryStatuses[status]}</span>
      {status === 'failed' && <button type="button" className="link-action" onClick={() => onRetry?.(item.id)}>{messages.retryDelivery}</button>}
    </div>
  );
}

export function TimelinePanel({ conversation, items, timezone, onRetryDelivery }: TimelinePanelProps) {
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
            <div className="message-meta"><strong>{kindLabel(item.kind)}</strong>
              <time dateTime={item.created_at}>{formatDate(item.created_at, timezone)}</time></div>
            {item.email && <EmailEnvelope email={item.email} />}
            <div className="markdown-body">{item.kind === 'incoming' ? <PlainTextBody text={item.body} /> : <Markdown>{item.body}</Markdown>}</div>
            <DeliveryStatus item={item} onRetry={onRetryDelivery} />
          </article>
        ))}
      </div>
    </section>
  );
}
