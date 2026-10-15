import { Fragment } from 'react';
import type { Conversation } from '../../api/schemas';
import { dayGroup, formatDate, formatShortDate } from '../../lib/format-date';
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
  filtered?: boolean;
  loading?: boolean;
  failed?: boolean;
  onRetry?: () => void;
  canManage?: boolean;
  onOpenSettings?: () => void;
}

const visibleTags = 2;

export function ConversationList({ items, selectedId, timezone, onSelect, page, hasNextPage, onPage, filtered = false, loading = false,
  failed = false, onRetry, canManage = false, onOpenSettings }: ConversationListProps) {
  // An empty list is only "new workspace" when it loaded; a failed request must never look empty.
  const settled = !loading && !failed;
  const brandNew = items.length === 0 && !filtered && settled && page === 1;
  const settingsLink = (label: string) => <a href="/settings" onClick={(event) => {
    if (!onOpenSettings) return;
    event.preventDefault();
    onOpenSettings();
  }}>{label}</a>;
  return (
    <div className="conversation-scroll">
      {brandNew && <section className="get-started" aria-label={messages.getStarted}>
        <h2>{messages.noConversationsYet}</h2>
        <p className="muted-copy">{messages.getStartedExplanation}</p>
        <ul>
          {canManage && <li>{settingsLink(messages.connectEmail)}</li>}
          <li><a href={messages.importFreshdeskURL} target="_blank" rel="noreferrer">{messages.importFreshdesk}</a></li>
          {canManage && <li>{settingsLink(messages.inviteTeam)}</li>}
        </ul>
      </section>}
      {failed && <div className="empty-copy"><p role="alert" className="error-copy">{messages.conversationsFailed}</p>
        <button type="button" onClick={() => onRetry?.()}>{messages.tryAgain}</button></div>}
      {items.length === 0 && !brandNew && settled && <p className="empty-copy">{messages.noConversations}</p>}
      {items.map((item, index) => {
        const group = dayGroup(item.updated_at, timezone);
        const startsGroup = index === 0 || group !== dayGroup(items[index - 1].updated_at, timezone);
        return (
          <Fragment key={item.id}>
          {startsGroup && <h2 className="group-label">{messages.dayGroups[group]}</h2>}
          <button className="conversation-row" data-selected={item.id === selectedId} onClick={() => onSelect?.(item.id)}>
            <span className="conversation-subject">{item.subject}</span>
            <time dateTime={item.updated_at} title={formatDate(item.updated_at, timezone)}>{formatShortDate(item.updated_at, timezone)}</time>
            <span className="conversation-meta">
              <StatusLabel status={item.status} />
              <PriorityLabel priority={item.priority} />
              {item.tags.slice(0, visibleTags).map((tag) => <span className="tag-pill" key={tag.id}>{tag.name}</span>)}
              {item.tags.length > visibleTags && <span className="tag-pill">+{item.tags.length - visibleTags}</span>}
            </span>
          </button>
          </Fragment>
        );
      })}
      {(page > 1 || hasNextPage) && <nav className="pagination" aria-label={messages.conversationList}>
        <button type="button" className="icon-button" aria-label={messages.previousPage} data-tip={messages.previousPage} disabled={page <= 1} onClick={() => onPage?.(page - 1)}><ChevronLeftIcon /></button>
        <span>{page}</span>
        <button type="button" className="icon-button" aria-label={messages.nextPage} data-tip={messages.nextPage} disabled={!hasNextPage} onClick={() => onPage?.(page + 1)}><ChevronRightIcon /></button>
      </nav>}
    </div>
  );
}
