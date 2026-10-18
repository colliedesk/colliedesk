import { useState } from 'react';
import Markdown from 'react-markdown';
import type { Attachment, Message, MessageOrigin } from '../../api/schemas';
import { formatBytes } from '../../lib/format-bytes';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { Avatar } from '../../ui/avatar';
import { LockIcon, PaperclipIcon } from '../../ui/icons';
import { EmailFrame } from './email-frame';
import { PlainTextBody } from './plain-text-body';

interface MessageItemProps {
  item: Message;
  timezone: string;
  onRetryDelivery?: (messageId: string) => void;
  // approvedBy names the person who sent a reply an AI agent drafted (the message's author).
  approvedBy?: string;
}

export function MessageItem({ item, timezone, onRetryDelivery, approvedBy }: MessageItemProps) {
  const [plainText, setPlainText] = useState(false);
  const showHTML = Boolean(item.body_html) && !plainText;
  // The stored raw email is offered on its own, not mixed with the customer's files.
  const originalID = item.email?.original_attachment_id ?? null;
  const stored = item.attachments.filter((file) => file.id !== originalID);
  // The server marks a file inline only when its bytes are embedded in the HTML. While the HTML is shown such
  // an image is already on screen, so its row would repeat it; the file stays stored and downloadable, and
  // the row returns in the plain-text view, where nothing else shows the image.
  const files = showHTML ? stored.filter((file) => !file.inline) : stored;
  const sender = approvedBy ?? item.author_name ?? messages.someone;
  // A token or draft line already names the person; the plain name would repeat it.
  const plainAuthor = item.author_name && !item.agent_token && !item.agent_request_id;
  return (
    <article className={`message message-${item.kind}`}>
      <header className="message-meta">
        <Avatar name={item.author_name ?? item.email?.from ?? kindLabel(item.kind)} />
        <div className="message-byline">
          {plainAuthor && <span className="message-author">{item.author_name}</span>}
          {!item.agent_token && item.agent_request_id && <span className="message-author">{messages.draftRewritten(sender)}</span>}
          {item.agent_token && <span className="message-author" title={item.agent_token.scope}>{item.agent_request_id
            ? messages.draftApproved(item.agent_token.label, sender)
            : messages.writtenByAgentToken(item.agent_token.agent_name, item.agent_token.label)}</span>}
          <span className="message-kind">{item.kind === 'note' && <LockIcon />}{kindLabel(item.kind)}</span>
        </div>
        <time dateTime={item.created_at}>{formatDate(item.created_at, timezone)}</time>
      </header>
      {item.email && <EmailEnvelope email={item.email} storedFiles={stored.length} />}
      {item.origin && <OriginLine origin={item.origin} />}
      {showHTML && item.body_html
        ? <EmailFrame title={messages.originalMessage} html={item.body_html} />
        : <div className="markdown-body">{item.kind === 'incoming' ? <PlainTextBody text={item.body} /> : <Markdown>{item.body}</Markdown>}</div>}
      {files.length > 0 && <AttachmentList items={files} />}
      {item.kind === 'outgoing' && item.cc.length > 0 && <div className="message-envelope"><span>{messages.ccLine(item.cc)}</span></div>}
      <DeliveryStatus item={item} onRetry={onRetryDelivery} />
      {item.body_html && (
        <div className="message-original">
          <button type="button" className="link-button" onClick={() => setPlainText(!plainText)}>
            {plainText ? messages.showFormattedEmail : messages.showPlainText}</button>
        </div>
      )}
    </article>
  );
}

function kindLabel(kind: Message['kind']) {
  if (kind === 'note') return messages.noteMode;
  return kind === 'incoming' ? messages.incoming : messages.replyMode;
}

function EmailEnvelope({ email, storedFiles }: { email: NonNullable<Message['email']>; storedFiles: number }) {
  // Anyone can type any address into a web form, so that label replaces the generic one.
  const warning = email.via === 'web_form' ? messages.webFormSender
    : email.authentication === 'fail' ? messages.senderFailedVerification
    : email.authentication === 'none' ? messages.senderNotVerified : undefined;
  return (
    <div className="message-envelope">
      <span>{email.from}</span>
      {email.cc && email.cc.length > 0 && <span>{messages.ccLine(email.cc)}</span>}
      {warning && <span className={`badge badge-${email.authentication}`}>{warning}</span>}
      {email.attachments.length > 0 && storedFiles === 0 && <span>{messages.attachmentsNotImported(email.attachments.map((item) => item.filename))}</span>}
      {email.original_attachment_id && <a href={`/api/v1/attachments/${email.original_attachment_id}`} download="original-message.eml.gz">{messages.downloadOriginalEmail}</a>}
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
      {status === 'sent' && lastError && <span>{lastError}</span>}
    </div>
  );
}

function OriginLine({ origin }: { origin: MessageOrigin }) {
  const system = messages.systemNames[origin.system] ?? origin.system;
  const lists = (['to', 'cc', 'bcc'] as const).filter((field) => origin[field].length > 0);
  return (
    <div className="message-origin">
      <span>{`${messages.originKinds[origin.kind]} ${system}`}</span>
      {lists.map((field) => <span key={field}>{`${messages.recipients[field]}: ${origin[field].join(', ')}`}</span>)}
    </div>
  );
}

function AttachmentList({ items }: { items: Attachment[] }) {
  return (
    <ul className="message-attachments" aria-label={messages.attachments}>
      {items.map((file) => (
        <li key={file.id}>
          <PaperclipIcon /><a href={`/api/v1/attachments/${file.id}`} download={file.filename} title={messages.downloadAttachment(file.filename)}>{file.filename}</a>
          <span className="attachment-size">{formatBytes(file.size)}</span>
        </li>
      ))}
    </ul>
  );
}
