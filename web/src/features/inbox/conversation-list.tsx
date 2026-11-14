import { Fragment, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import type { Conversation, ConversationViewer } from '../../api/schemas';
import { dayGroup, formatDate, formatListTime, formatShortDate, zoneSuffix } from '../../lib/format-date';
import { useNow } from '../../lib/use-now';
import { messages } from '../../messages';
import { Part } from '../../ui/lazy-part';
import { ChevronLeftIcon, ChevronRightIcon } from '../../ui/icons';
import { ownRef } from './conversation-ref';
import { helpTipPart } from './help-tip-part';
import { rowDue, slaOf } from '../../lib/sla';
import { presenceViewPart } from './presence-part';
import { useListKeys } from './use-list-keys';
import { isTyping } from '../../lib/typing-target';

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
  // checkedIds are the rows ticked for a bulk action; ticking never opens the conversation. range: shift was
  // held, so every row from the last ticked one to this one takes its new state.
  checkedIds?: ReadonlySet<string>;
  onToggle?: (id: string, range?: boolean) => void;
  // draftIds are the conversations with a reply draft an AI agent left for a person to send.
  draftIds?: ReadonlySet<string>;
  // viewers are the others who have a conversation open now, by conversation id (the activity poll).
  viewers?: ReadonlyMap<string, ConversationViewer[]>;
  // grouped splits the rows under Today and Earlier; only the latest-activity order keeps the groups contiguous.
  grouped?: boolean;
  // Sends the agent a test message on the default email channel; only where the workspace has one.
  onTestMessage?: () => Promise<unknown>;
  // The first setup step the person may do, as the one primary button of a brand-new workspace.
  nextStep?: { label: string; href: string };
}

// Where the inbox sends an admin to finish something in Settings.
export type SettingsTarget = '/settings' | '/settings/data' | `/settings/channels/${string}`;

// dotOf is a row's status dot: red for an urgent or late conversation, the status colour otherwise, with
// the status (and an urgent priority) in words for a screen reader.
function dotOf(item: Conversation, late: boolean): { tone: string; words: string } {
  const status = messages.statuses[item.status];
  if (item.priority === 'urgent') return { tone: 'urgent', words: `${status}, ${messages.priorities.urgent.toLowerCase()}` };
  return { tone: late ? 'urgent' : item.status, words: status };
}

export function ConversationList({ items, channelLabel, selectedId, timezone, onSelect, page, hasNextPage, onPage, filtered = false, nothingOpen = false, loading = false, refreshing = false,
  failed = false, onRetry, onClearFilters, checkedIds, onToggle, draftIds, viewers, grouped = true, onTestMessage, nextStep }: ConversationListProps) {
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
  const selecting = items.some((item) => checkedIds?.has(item.id));
  const scroll = useRef<HTMLDivElement>(null);
  const onArrows = useListKeys(scroll, items.map((item) => item.id), selectedId, onSelect);
  // x ticks the row that has the focus, as a click on its box does.
  const onListKey = (event: KeyboardEvent<HTMLDivElement>) => {
    const focused = event.target instanceof Element ? event.target.closest<HTMLElement>('[data-conversation-id]')?.dataset.conversationId : undefined;
    if (event.key === 'x' && focused && onToggle && !event.ctrlKey && !event.metaKey && !event.altKey && !isTyping(event.target)) {
      event.preventDefault();
      onToggle(focused);
      return;
    }
    onArrows(event);
  };
  // Shift on the click that ticks a box makes it a range; the change event that follows does not carry it.
  const shift = useRef(false);
  return (
    <div className="conversation-scroll" aria-busy={refreshing} ref={scroll} onKeyDown={onListKey} data-selecting={selecting || undefined}>
      {loading && items.length === 0 && <p role="status" className="empty-copy">{messages.loading}</p>}
      {brandNew && <section className="get-started" aria-label={messages.getStarted}>
        <h2>{messages.noConversationsYet}</h2>
        <p className="muted-copy">{messages.getStartedExplanation}</p>
        {nextStep && <p><a className="button primary-action" href={nextStep.href}>{nextStep.label}</a></p>}
        {onTestMessage && <p><button type="button" className="button" disabled={testing} onClick={sendTest}>{messages.sendTestMessage}</button>{' '}
          <span className="muted-copy">{messages.sendTestMessageHint}</span>{testFailed && <span role="alert" className="error-copy"> {testFailed}</span>}</p>}
      </section>}
      {failed === 'results_refreshed' && <div className="empty-copy"><p className="heading-with-tip"><span role="alert" className="error-copy">{messages.resultsRefreshed}</span>
        <Part part={helpTipPart} render={(tip) => <tip.HelpTip label={messages.resultsRefreshedAbout} text={messages.resultsRefreshedWhy} />} /></p>
        <div className="button-row"><a href="/results" className="button">{messages.openResults}</a>
          {onClearFilters && <button type="button" onClick={onClearFilters}>{messages.clearFilter}</button>}</div></div>}
      {failed === true && <div className="empty-copy"><p role="alert" className="error-copy">{messages.conversationsFailed}</p>
        <button type="button" onClick={() => onRetry?.()}>{messages.tryAgain}</button></div>}
      {items.length === 0 && !brandNew && settled && <div className="empty-copy"><p>{nothingOpen ? messages.noOpenConversations : messages.noConversations}</p>
        {filtered && onClearFilters && <button type="button" onClick={onClearFilters}>{messages.clearFilters}</button>}</div>}
      {items.map((item, index) => {
        const group = dayGroup(item.updated_at, timezone, now);
        const startsGroup = grouped && (index === 0 || group !== dayGroup(items[index - 1].updated_at, timezone, now));
        // The row's time is when the customer last wrote: the team's own notes and replies never move it.
        // A conversation the customer never wrote in shows its last update. The tooltip names the timezone
        // when it is not the browser's, so 5:42 AM is never read as local time.
        const wrote = item.latest_customer_message?.created_at;
        const stamp = wrote ?? item.updated_at;
        const full = (wrote ? messages.customerWrote : '') + formatDate(stamp, timezone, now) + zoneSuffix(stamp, timezone);
        const open = viewers?.get(item.id);
        const due = rowDue(slaOf(item), timezone, now);
        const dot = dotOf(item, due?.late ?? false);
        return (
          <Fragment key={item.id}>
          {startsGroup && <h2 className="group-label">{messages.dayGroups[group]}</h2>}
          <div className="conversation-item" data-conversation-id={item.id}>
          <label className="row-check">
            <input type="checkbox" aria-label={messages.selectRow(item.contact_name, item.subject, full)} checked={checkedIds?.has(item.id) ?? false}
              onClick={(event) => { shift.current = event.shiftKey; }} onChange={() => { onToggle?.(item.id, shift.current); shift.current = false; }} />
          </label>
          <button className="conversation-row" aria-current={item.id === selectedId ? true : undefined} onClick={() => onSelect?.(item.id)}>
            <span className="row-top">
              <span className={`row-dot row-dot-${dot.tone}`}>{dot.words}</span>
              <span className="row-customer">{item.contact_name}</span>
              {channelLabel?.(item) && <span className="channel-pill">{channelLabel(item)}</span>}
              {draftIds?.has(item.id) && <span className="badge badge-draft">{messages.draftWaiting}</span>}
              {open && open.length > 0 && <Part part={presenceViewPart} render={(view) => <view.ViewerStack viewers={open} timezone={timezone} />} />}
              {due ? <span className={`row-due${due.late ? ' row-due-late' : ''}`} aria-label={due.full} data-tip={due.full}>{due.text}</span>
                : <time dateTime={stamp} data-tip={full}>
                  <span aria-hidden="true" className="time-short">{formatShortDate(stamp, timezone, now)}</span>
                  <span aria-hidden="true" className="time-touch">{formatListTime(stamp, timezone, now)}</span>
                </time>}
            </span>
            <span className="row-subject">{ownRef(item) && <span className="conv-ref">{ownRef(item)}</span>}<span className="conversation-subject">{item.subject}</span></span>
            {item.latest_customer_message && <span className="row-excerpt">{item.latest_customer_message.excerpt}</span>}
            <span className="time-full">{full}</span>
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
