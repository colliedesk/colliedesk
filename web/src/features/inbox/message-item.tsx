import { useState } from 'react';
import Markdown from 'react-markdown';
import type { Attachment, Message, MessageOrigin } from '../../api/schemas';
import { formatBytes } from '../../lib/format-bytes';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';

// Imported HTML is untrusted: it is shown only in a sandboxed frame (no
// scripts, unique origin) whose policy blocks every network load.
const sandboxPolicy = "default-src 'none'; style-src 'unsafe-inline'; img-src data:";

export function originalDocument(html: string): string {
  return `<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${sandboxPolicy}">${html}`;
}

export function MessageItem({ item, timezone }: { item: Message; timezone: string }) {
  const [showOriginal, setShowOriginal] = useState(false);
  return (
    <article className={`message message-${item.kind}`}>
      <div className="message-meta"><span><strong>{item.kind === 'note' ? messages.noteMode : item.kind === 'incoming' ? messages.incoming : messages.replyMode}</strong>
        {item.author_name && <span className="message-author">{item.author_name}</span>}</span>
        <time dateTime={item.created_at}>{formatDate(item.created_at, timezone)}</time></div>
      {item.origin && <OriginLine origin={item.origin} />}
      <div className="markdown-body"><Markdown>{item.body}</Markdown></div>
      {item.attachments.length > 0 && <AttachmentList items={item.attachments} />}
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
