import './mentions.css';
import type { Mention } from '../../api/schemas';
import { formatShortDate, stampTitle } from '../../lib/format-date';
import { mentionMessages } from './mention-messages';

export type MentionShow = 'unread' | 'all';

interface MentionsListProps {
  items: Mention[];
  // How many of the person's mentions are unread, in all (the Unread tab's number).
  unread: number;
  show: MentionShow;
  onShow: (show: MentionShow) => void;
  onOpen: (item: Mention) => void;
  onMarkAllRead: () => void;
  timezone: string;
  // The conversation open beside the list.
  selectedConversation?: string;
  loading?: boolean;
  failed?: boolean;
  onRetry?: () => void;
  onOlder?: () => void;
  marking?: boolean;
}

// mentionHref is where a mention opens: its conversation beside the Mentions list, at the note.
export const mentionHref = (item: Pick<Mention, 'conversation_id' | 'message_id'>) => `/mentions/${item.conversation_id}#message-${item.message_id}`;

// whoAsked is the person who wrote the note, with the agent token when one wrote it for them.
export const whoAsked = (item: Mention) => {
  const person = item.author_name ?? mentionMessages.someone;
  return item.agent_token ? mentionMessages.via(person, item.agent_token.label) : person;
};

// MentionsList is the list column of Mentions (I9-c): Unread N / All / Mark all read, then one row per note that
// asked the person, newest first, with who asked, the conversation, the quoted note, the time and an unread dot.
export function MentionsList({ items, unread, show, onShow, onOpen, onMarkAllRead, timezone, selectedConversation, loading, failed, onRetry, onOlder, marking }: MentionsListProps) {
  return (
    <div className="mentions-list">
      <div className="mentions-toolbar" role="group" aria-label={mentionMessages.show}>
        <button type="button" className="mentions-tab" aria-pressed={show === 'unread'} onClick={() => onShow('unread')}>{mentionMessages.unreadTab(unread)}</button>
        <button type="button" className="mentions-tab" aria-pressed={show === 'all'} onClick={() => onShow('all')}>{mentionMessages.allTab}</button>
        <button type="button" className="link-action mentions-mark" disabled={unread === 0 || marking} onClick={onMarkAllRead}>{mentionMessages.markAllRead}</button>
      </div>
      <div className="conversation-scroll">
        {loading && items.length === 0 && <p role="status" className="empty-copy">…</p>}
        {failed && <div className="empty-copy"><p role="alert" className="error-copy">{mentionMessages.loadFailed}</p>
          <button type="button" onClick={onRetry}>{mentionMessages.tryAgain}</button></div>}
        {!loading && !failed && items.length === 0 && <p className="empty-copy">{show === 'unread' ? mentionMessages.noUnread : mentionMessages.noneYet}</p>}
        {items.map((item) => (
          <a key={item.id} href={mentionHref(item)} className="mention-row" data-unread={!item.read_at || undefined}
            aria-current={item.conversation_id === selectedConversation ? true : undefined}
            onClick={(event) => { if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return; event.preventDefault(); onOpen(item); }}>
            <span className="row-top">
              <span className="mention-dot" aria-hidden={item.read_at ? true : undefined}>{!item.read_at && <span className="sr-only">{mentionMessages.unread}</span>}</span>
              <span className="row-customer">{whoAsked(item)}</span>
              <time dateTime={item.created_at} title={stampTitle(item.created_at, timezone)}>{formatShortDate(item.created_at, timezone)}</time>
            </span>
            <span className="row-subject">{item.display_ref && <span className="conv-ref">{item.display_ref}</span>}<span className="conversation-subject">{item.subject}</span></span>
            <span className="row-excerpt">{`“${item.excerpt}”`}</span>
          </a>
        ))}
        {onOlder && <button type="button" className="link-action mentions-older" onClick={onOlder}>{mentionMessages.older}</button>}
      </div>
    </div>
  );
}
