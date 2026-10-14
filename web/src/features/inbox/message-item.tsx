import { useState } from 'react';
import Markdown from 'react-markdown';
import type { Attachment, Message, MessageOrigin } from '../../api/schemas';
import { formatBytes } from '../../lib/format-bytes';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { PlainTextBody } from './plain-text-body';

// Imported HTML is untrusted. It is shown only in a sandboxed frame (no
// scripts, unique origin) whose policy blocks every load, form and frame, and
// the markup is stripped of everything that can navigate or fetch: CSP alone
// does not stop <meta http-equiv="refresh"> or a clicked link.
const sandboxPolicy = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; form-action 'none'; base-uri 'none'; frame-src 'none'";
const removedElements = 'meta, base, link, script, noscript, iframe, frame, frameset, object, embed, form, portal';
const urlAttributes = ['href', 'src', 'srcset', 'action', 'formaction', 'xlink:href', 'ping', 'background', 'poster', 'cite', 'longdesc', 'target'];

function sanitizeOriginal(html: string): string {
  const parsed = new DOMParser().parseFromString(html, 'text/html');
  parsed.querySelectorAll(removedElements).forEach((element) => element.remove());
  parsed.querySelectorAll('*').forEach((element) => {
    for (const attribute of Array.from(element.attributes)) {
      const name = attribute.name.toLowerCase();
      const inlineImage = name === 'src' && element.tagName === 'IMG' && attribute.value.trim().startsWith('data:image/');
      if (name.startsWith('on') || (urlAttributes.includes(name) && !inlineImage)) element.removeAttribute(attribute.name);
    }
  });
  const styles = Array.from(parsed.head.querySelectorAll('style')).map((style) => style.outerHTML).join('');
  return styles + parsed.body.innerHTML;
}

export function originalDocument(html: string): string {
  return `<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${sandboxPolicy}">${sanitizeOriginal(html)}`;
}

interface MessageItemProps {
  item: Message;
  timezone: string;
  onRetryDelivery?: (messageId: string) => void;
}

export function MessageItem({ item, timezone, onRetryDelivery }: MessageItemProps) {
  const [showOriginal, setShowOriginal] = useState(false);
  return (
    <article className={`message message-${item.kind}`}>
      <div className="message-meta"><span><strong>{kindLabel(item.kind)}</strong>
        {item.author_name && <span className="message-author">{item.author_name}</span>}</span>
        <time dateTime={item.created_at}>{formatDate(item.created_at, timezone)}</time></div>
      {item.email && <EmailEnvelope email={item.email} />}
      {item.origin && <OriginLine origin={item.origin} />}
      <div className="markdown-body">{item.kind === 'incoming' ? <PlainTextBody text={item.body} /> : <Markdown>{item.body}</Markdown>}</div>
      {item.attachments.length > 0 && <AttachmentList items={item.attachments} />}
      <DeliveryStatus item={item} onRetry={onRetryDelivery} />
      {item.body_html && (
        <div className="message-original">
          <button type="button" className="link-button" onClick={() => setShowOriginal(!showOriginal)}>
            {showOriginal ? messages.hideOriginal : messages.showOriginal}</button>
          {showOriginal && <iframe title={messages.originalMessage} sandbox="" srcDoc={originalDocument(item.body_html)} />}
        </div>
      )}
    </article>
  );
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
          <a href={`/api/v1/attachments/${file.id}`} download={file.filename} title={messages.downloadAttachment(file.filename)}>{file.filename}</a>
          <span className="attachment-size">{formatBytes(file.size)}</span>
        </li>
      ))}
    </ul>
  );
}
