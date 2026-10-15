import { Fragment, useEffect, useRef } from 'react';
import type { Conversation } from '../../api/schemas';
import { dayGroup, formatDate, formatShortDate } from '../../lib/format-date';
import { useNow } from '../../lib/use-now';
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
  // checkedIds are the rows ticked for a bulk action; ticking never opens the conversation.
  checkedIds?: ReadonlySet<string>;
  onToggle?: (id: string) => void;
  onToggleAll?: (checked: boolean) => void;
}

const visibleTags = 2;

export function ConversationList({ items, selectedId, timezone, onSelect, page, hasNextPage, onPage, filtered = false, loading = false,
  failed = false, onRetry, canManage = false, onOpenSettings, checkedIds, onToggle, onToggleAll }: ConversationListProps) {
  // An empty list is only "new workspace" when it loaded; a failed request must never look empty.
  const now = useNow(timezone);
  const settled = !loading && !failed;
  const brandNew = items.length === 0 && !filtered && settled && page === 1;
  const checkedCount = items.filter((item) => checkedIds?.has(item.id)).length;
  const allChecked = items.length > 0 && checkedCount === items.length;
  const selectAll = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (selectAll.current) selectAll.current.indeterminate = checkedCount > 0 && !allChecked;
  }, [checkedCount, allChecked]);
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
      {items.length > 0 && <label className="select-all">
        <input type="checkbox" ref={selectAll} checked={allChecked} onChange={(event) => onToggleAll?.(event.target.checked)} />
        <span>{messages.selectAllOnPage}</span>
      </label>}
      {items.map((item, index) => {
        const group = dayGroup(item.updated_at, timezone, now);
        const startsGroup = index === 0 || group !== dayGroup(items[index - 1].updated_at, timezone, now);
        const full = formatDate(item.updated_at, timezone, now);
        return (
          <Fragment key={item.id}>
          {startsGroup && <h2 className="group-label">{messages.dayGroups[group]}</h2>}
          <div className="conversation-item">
          <label className="row-check">
            <input type="checkbox" aria-label={messages.selectRow(item.subject)} checked={checkedIds?.has(item.id) ?? false} onChange={() => onToggle?.(item.id)} />
          </label>
          <button className="conversation-row" data-selected={item.id === selectedId} data-tip={full} onClick={() => onSelect?.(item.id)}>
            <span className="conversation-subject">{item.subject}</span>
            <time dateTime={item.updated_at}>
              <span aria-hidden="true" className="time-short">{formatShortDate(item.updated_at, timezone, now)}</span>
              <span className="time-full">{full}</span>
            </time>
            <span className="conversation-meta">
              <StatusLabel status={item.status} />
              <PriorityLabel priority={item.priority} />
              {item.tags.slice(0, visibleTags).map((tag) => <span className="tag-pill" key={tag.id}>{tag.name}</span>)}
              {item.tags.length > visibleTags && <span className="tag-pill">+{item.tags.length - visibleTags}</span>}
            </span>
          </button>
          </div>
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
