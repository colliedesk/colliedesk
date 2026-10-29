import { Fragment, useEffect, useRef, useState } from 'react';
import type { ReactNode, Ref } from 'react';
import type { Conversation, ConversationViewer } from '../../api/schemas';
import { dayGroup, formatDate, formatListTime, formatShortDate, stampTitle, zoneSuffix } from '../../lib/format-date';
import { useNow } from '../../lib/use-now';
import { messages } from '../../messages';
import { Part } from '../../ui/lazy-part';
import { ChevronLeftIcon, ChevronRightIcon } from '../../ui/icons';
import { PriorityLabel, StatusLabel } from './conversation-labels';
import { presenceViewPart } from './presence-part';
import { useListKeys } from './use-list-keys';

interface ConversationListProps {
  items: Conversation[];
  // Names a conversation's channel where the workspace has two of its type.
  channelLabel?: (conversation: Conversation) => string | undefined;
  selectedId?: string;
  timezone: string;
  onSelect?: (id: string) => void;
  page: number;
  hasNextPage: boolean;
  onPage?: (page: number) => void;
  filtered?: boolean;
  // The Open view is empty but the workspace has conversations: caught up, not new.
  nothingOpen?: boolean;
  loading?: boolean;
  // refreshing: a refetch is running while the previous rows stay on screen.
  refreshing?: boolean;
  failed?: boolean | 'results_refreshed';
  onRetry?: () => void;
  onClearFilters?: () => void;
  // checkedIds are the rows ticked for a bulk action; ticking never opens the conversation.
  checkedIds?: ReadonlySet<string>;
  onToggle?: (id: string) => void;
  onToggleAll?: (checked: boolean) => void;
  selectAllRef?: Ref<HTMLInputElement>;
  // toolbar takes the place of the select all caption while rows are ticked.
  toolbar?: ReactNode;
  // draftIds are the conversations with a reply draft an AI agent left for a person to send.
  draftIds?: ReadonlySet<string>;
  // viewers are the others who have a conversation open now, by conversation id (the activity poll).
  viewers?: ReadonlyMap<string, ConversationViewer[]>;
  // grouped splits the rows under Today and Earlier; only the latest-activity order keeps the groups contiguous.
  grouped?: boolean;
  // guide is the setup guide, shown above the rows while setup is unfinished (also when conversations exist).
  guide?: ReactNode;
  // Sends the agent a test message on the default email channel; only where the workspace has one.
  onTestMessage?: () => Promise<unknown>;
}

// Where the inbox sends an admin to finish something in Settings.
export type SettingsTarget = '/settings' | '/settings/import-freshdesk' | `/settings/channels/${string}`;

const visibleTags = 2;
export function ConversationList({ items, channelLabel, selectedId, timezone, onSelect, page, hasNextPage, onPage, filtered = false, nothingOpen = false, loading = false, refreshing = false,
  failed = false, onRetry, onClearFilters, checkedIds, onToggle, onToggleAll, selectAllRef, toolbar, draftIds, viewers, grouped = true, guide, onTestMessage }: ConversationListProps) {
  const [testing, setTesting] = useState(false);
  const [testFailed, setTestFailed] = useState<string>();
  const testInFlight = useRef(false);
  const sendTest = () => {
    if (testInFlight.current) return;
    testInFlight.current = true;
    setTesting(true);
    setTestFailed(undefined);
    onTestMessage?.().catch((error: unknown) => setTestFailed(error instanceof Error && !error.message.startsWith('API ') ? error.message : messages.testMessageFailed))
      .finally(() => { testInFlight.current = false; setTesting(false); });
  };
  // An empty list is only "new workspace" when it loaded; a failed request must never look empty.
  const now = useNow(timezone);
  const settled = !loading && !failed;
  const brandNew = items.length === 0 && !filtered && !nothingOpen && settled && page === 1;
  const checkedCount = items.filter((item) => checkedIds?.has(item.id)).length;
  const allChecked = items.length > 0 && checkedCount === items.length;
  const selectAll = useRef<HTMLInputElement | null>(null);
  const setSelectAll = (node: HTMLInputElement | null) => {
    selectAll.current = node;
    if (typeof selectAllRef === 'function') selectAllRef(node);
    else if (selectAllRef) selectAllRef.current = node;
  };
  useEffect(() => {
    if (selectAll.current) selectAll.current.indeterminate = checkedCount > 0 && !allChecked;
  }, [checkedCount, allChecked]);
  const scroll = useRef<HTMLDivElement>(null);
  const onListKey = useListKeys(scroll, items.map((item) => item.id), selectedId, onSelect);
  return (
    <div className="conversation-scroll" aria-busy={refreshing} ref={scroll} onKeyDown={onListKey}>
      {guide}
      {loading && items.length === 0 && <p role="status" className="empty-copy">{messages.loading}</p>}
      {brandNew && <section className="get-started" aria-label={messages.getStarted}>
        <h2>{messages.noConversationsYet}</h2>
        <p className="muted-copy">{messages.getStartedExplanation}</p>
        {onTestMessage && <p><button type="button" className="button" disabled={testing} onClick={sendTest}>{messages.sendTestMessage}</button>{' '}
          <span className="muted-copy">{messages.sendTestMessageHint}</span>{testFailed && <span role="alert" className="error-copy"> {testFailed}</span>}</p>}
      </section>}
      {failed === 'results_refreshed' && <div className="empty-copy"><p role="alert" className="error-copy">{messages.resultsRefreshed}</p>
        <p className="muted-copy">{messages.resultsRefreshedWhy}</p>
        <div className="button-row"><a href="/results" className="button">{messages.openResults}</a>
          {onClearFilters && <button type="button" onClick={onClearFilters}>{messages.clearFilter}</button>}</div></div>}
      {failed === true && <div className="empty-copy"><p role="alert" className="error-copy">{messages.conversationsFailed}</p>
        <button type="button" onClick={() => onRetry?.()}>{messages.tryAgain}</button></div>}
      {items.length === 0 && !brandNew && settled && <div className="empty-copy"><p>{nothingOpen ? messages.noOpenConversations : messages.noConversations}</p>
        {filtered && onClearFilters && <button type="button" onClick={onClearFilters}>{messages.clearFilters}</button>}</div>}
      {items.length > 0 && <div className="select-all" data-active={toolbar ? true : undefined}>
        <label>
          <input type="checkbox" ref={setSelectAll} checked={allChecked} onChange={(event) => onToggleAll?.(event.target.checked)} />
          <span className={toolbar ? 'sr-only' : undefined}>{messages.selectAllOnPage}</span>
        </label>
        {toolbar}
      </div>}
      {items.map((item, index) => {
        const group = dayGroup(item.updated_at, timezone, now);
        const startsGroup = grouped && (index === 0 || group !== dayGroup(items[index - 1].updated_at, timezone, now));
        // The tooltip names the timezone when it is not the browser's, so 5:42 AM is never read as local time.
        const full = formatDate(item.updated_at, timezone, now) + zoneSuffix(item.updated_at, timezone);
        const open = viewers?.get(item.id);
        return (
          <Fragment key={item.id}>
          {startsGroup && <h2 className="group-label">{messages.dayGroups[group]}</h2>}
          <div className="conversation-item" data-conversation-id={item.id}>
          <label className="row-check">
            <input type="checkbox" aria-label={messages.selectRow(item.subject, full)} checked={checkedIds?.has(item.id) ?? false} onChange={() => onToggle?.(item.id)} />
          </label>
          <button className="conversation-row" aria-current={item.id === selectedId ? true : undefined} data-tip={full} onClick={() => onSelect?.(item.id)}>
            <span className="conversation-subject">{item.subject}</span>
            <time dateTime={item.updated_at} title={stampTitle(item.updated_at, timezone)}>
              <span aria-hidden="true" className="time-short">{formatShortDate(item.updated_at, timezone, now)}</span>
              <span aria-hidden="true" className="time-touch">{formatListTime(item.updated_at, timezone, now)}</span>
              <span className="time-full">{full}</span>
            </time>
            <span className="conversation-meta">
              <StatusLabel status={item.status} />
              <PriorityLabel priority={item.priority} />
              {channelLabel?.(item) && <span className="channel-pill">{channelLabel(item)}</span>}
              {draftIds?.has(item.id) && <span className="badge badge-draft">{messages.draftWaiting}</span>}
              {open && open.length > 0 && <Part part={presenceViewPart} render={(view) => <view.ViewerStack viewers={open} timezone={timezone} />} />}
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
