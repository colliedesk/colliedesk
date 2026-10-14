import type { Conversation } from '../../api/schemas';
import { formatDate, formatShortDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { ChevronLeftIcon, ChevronRightIcon } from '../../ui/icons';
import { PriorityLabel, StatusLabel } from './conversation-labels';

interface ConversationListProps {
  items: Conversation[];
  selectedId?: string;
  timezone: string;
  onSelect?: (id: string) => void;
  page: number;
  hasNextPage: boolean;
  onPage?: (page: number) => void;
}

const visibleTags = 2;

export function ConversationList({ items, selectedId, timezone, onSelect, page, hasNextPage, onPage }: ConversationListProps) {
  return (
    <div className="conversation-scroll">
      {items.length === 0 && <p className="empty-copy">{messages.noConversations}</p>}
      {items.map((item) => (
        <button className="conversation-row" data-selected={item.id === selectedId} key={item.id} onClick={() => onSelect?.(item.id)}>
          <span className="conversation-subject">{item.subject}</span>
          <time dateTime={item.updated_at} title={formatDate(item.updated_at, timezone)}>{formatShortDate(item.updated_at, timezone)}</time>
          <span className="conversation-meta">
            <StatusLabel status={item.status} />
            <PriorityLabel priority={item.priority} />
            {item.tags.slice(0, visibleTags).map((tag) => <span className="tag-pill" key={tag.id}>{tag.name}</span>)}
            {item.tags.length > visibleTags && <span className="tag-pill">+{item.tags.length - visibleTags}</span>}
          </span>
        </button>
      ))}
      {(page > 1 || hasNextPage) && <nav className="pagination" aria-label={messages.conversationList}>
        <button type="button" className="icon-button" aria-label={messages.previousPage} disabled={page <= 1} onClick={() => onPage?.(page - 1)}><ChevronLeftIcon /></button>
        <span>{page}</span>
        <button type="button" className="icon-button" aria-label={messages.nextPage} disabled={!hasNextPage} onClick={() => onPage?.(page + 1)}><ChevronRightIcon /></button>
      </nav>}
    </div>
  );
}
