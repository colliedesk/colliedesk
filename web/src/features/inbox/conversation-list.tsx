import type { Conversation } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';

interface ConversationListProps {
  items: Conversation[];
  selectedId?: string;
  timezone: string;
  onSelect?: (id: string) => void;
  page: number;
  hasNextPage: boolean;
  onPage?: (page: number) => void;
}

export function ConversationList({ items, selectedId, timezone, onSelect, page, hasNextPage, onPage }: ConversationListProps) {
  return (
    <section className="conversation-list" aria-label={messages.conversationList}>
      <header><h1>{messages.inbox}</h1><span>{items.length}</span></header>
      {items.length === 0 && <p className="empty-copy">{messages.noConversations}</p>}
      {items.map((item) => (
        <button className="conversation-row" data-selected={item.id === selectedId} key={item.id} onClick={() => onSelect?.(item.id)}>
          <span className="conversation-subject">{item.subject}</span>
          <time dateTime={item.updated_at}>{formatDate(item.updated_at, timezone)}</time>
          <span className={`status status-${item.status}`}>{messages.statuses[item.status]}</span>
          <span className="priority">{messages.priorities[item.priority]}</span>
        </button>
      ))}
      <nav className="pagination" aria-label={messages.conversationList}>
        <button type="button" disabled={page <= 1} onClick={() => onPage?.(page - 1)}>{messages.previousPage}</button>
        <span>{page}</span>
        <button type="button" disabled={!hasNextPage} onClick={() => onPage?.(page + 1)}>{messages.nextPage}</button>
      </nav>
    </section>
  );
}
