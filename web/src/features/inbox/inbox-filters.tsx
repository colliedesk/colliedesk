import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { messages } from '../../messages';
import type { Conversation, Tag, TeamMember } from '../../api/schemas';
import { ArrowUpDownIcon, FilterIcon, SearchIcon } from '../../ui/icons';
import { allStatuses, defaultStatus } from '../../lib/inbox-query';
import type { InboxQuery } from '../../lib/inbox-query';
import { DraftInput } from './draft-input';
import type { FilterPartProps } from './filter-panel';
import { lazyPart, PartUnavailable } from '../../ui/lazy-part';

interface InboxFiltersProps {
  query: InboxQuery;
  onChange?: (query: InboxQuery) => void;
  tags: Tag[];
  agents?: TeamMember[];
  // Dates are picked as calendar days in this timezone and sent as UTC instants.
  timezone?: string;
  // The rows on screen, so a customer filter reads as the customer's name.
  conversations?: Conversation[];
  // The signed-in agent, listed as Me in the Filter menu.
  currentAgentId?: string;
  focusSearch?: boolean;
  // Bumped to move focus to the search box, for example after the list's Clear filters.
  focusSignal?: number;
  // The saved views menu, which sits in the same bar.
  views?: ReactNode;
  // Select all and the bulk actions, in the place of the status tabs while rows are ticked.
  selection?: ReactNode;
  // How many conversations each status holds under the other filters (status_counts).
  counts?: Partial<Record<string, number>>;
}

// The Filter menu, the Sort menu and the chips load on their own chunk (filter-panel.tsx), asked for as
// soon as the inbox renders, so the first bundle does not carry them. Until they arrive, the search box
// and the status tabs work, and inert controls of the same size hold their places.
export const filterPart = lazyPart(() => import('./filter-panel'));
// A search is sent once the person pauses this long, so a word is one request, not one per key.
export const typingDelayMs = 100;

function FilterBarPart(props: FilterPartProps) {
  const part = filterPart.use();
  if (part.status === 'ready') return <part.module.FilterBar {...props} />;
  if (part.status === 'error') return <PartUnavailable />;
  const sort = <span className="sort-pick"><ArrowUpDownIcon /><select className="sort-field" disabled aria-label={messages.inboxSort} /></span>;
  return <>{props.sortFirst && sort}<button type="button" className="filter-menu-pending icon-button" disabled><FilterIcon /><span className="sr-only">{messages.filter}</span></button>
    {!props.sortFirst && sort}</>;
}

function FilterChipsPart(props: FilterPartProps) {
  const part = filterPart.use();
  // Until the chips arrive, an empty row of their height holds their place, so the list does not jump.
  if (part.status === 'ready') return <part.module.FilterChips {...props} />;
  return part.status === 'error' ? null : <div className="filter-chips filter-chips-pending" aria-hidden="true" />;
}

const statusTabs = [[allStatuses, messages.allConversations], ...Object.entries(messages.statuses)] as const;
// Keys with controls of their own; any other filter shows as a chip.
const ownControls = ['status', 'sort', 'search'];
// The tabs an agent needs every day stay; the rest show when they mean something (Paper "15 Inbox": I1-a, I4-a,
// I4-b, I10-d). All and Closed join a search or a filter, whose results span every status; Spam shows once it
// holds a conversation; the tab on screen always shows, so a link to any status keeps its tab.
function showsTab(value: string, status: string, narrowed: boolean, counts?: Partial<Record<string, number>>) {
  if (value === status) return true;
  if (value === allStatuses || value === 'closed') return narrowed;
  if (value === 'spam') return (counts?.spam ?? 0) > 0;
  return true;
}

// At rest the list starts on one row: the status tabs on the left, Sort and Filter as icons on the right. While a
// search or a filter is on, the search box takes a row of its own above the tabs, with Filter (and its count), Sort and
// the saved views beside it, and each active filter shows as a chip below the tabs that removes it. Search starts from
// the app's search (it opens this box through focusSignal, or links here with ?search=). The order on screen is the
// order for Tab. Status is the view an agent switches most, so it is a row of tabs; everything else shares one Filter menu.
export function InboxFilters({ query, onChange, tags, agents = [], timezone = 'UTC', conversations, currentAgentId, focusSearch = false, focusSignal = 0, views, selection, counts }: InboxFiltersProps) {
  const search = useRef<HTMLInputElement>(null);
  const tabsRef = useRef<HTMLDivElement>(null);
  // The search box also shows while the person uses it: opened by the app's Search or by Clear filters, typed in,
  // or focused. It closes again once it is empty and the focus leaves the filters, or on Escape.
  const [open, setOpen] = useState(focusSearch || focusSignal > 0);
  const [seenSignal, setSeenSignal] = useState(focusSignal);
  if (focusSignal !== seenSignal) {
    setSeenSignal(focusSignal);
    setOpen(true);
  }
  // Escape closes an empty box and leaves the focus on the chosen status tab, in the row that replaced it.
  const [closedByKey, setClosedByKey] = useState(0);
  useEffect(() => {
    if (closedByKey > 0) tabsRef.current?.querySelector<HTMLInputElement>('input:checked')?.focus();
  }, [closedByKey]);
  useEffect(() => {
    if (focusSignal > 0) search.current?.focus();
  }, [focusSignal]);
  const setStatus = (value: string) => {
    const next: InboxQuery = { ...query, status: value };
    // Open with no status picked stays the default address.
    if (value === defaultStatus && query.status === undefined) delete next.status;
    onChange?.(next);
  };
  const onSearch = (value: string) => {
    setOpen(true);
    const next: InboxQuery = { ...query, search: value };
    if (!value) delete next.search;
    // Search looks through every status unless the agent chose one: an imported ticket is usually closed.
    if (value && query.status === undefined) next.status = allStatuses;
    onChange?.(next);
  };
  const part = { query, onChange, tags, agents, timezone, conversations, currentAgentId };
  const status = query.status ?? defaultStatus;
  const narrowed = Object.keys(query).some((key) => key !== 'status' && key !== 'sort');
  const filtered = Object.keys(query).some((key) => !ownControls.includes(key));
  const searchRow = Boolean(query.search) || filtered || open;
  const closeIfEmpty = () => {
    if (!search.current?.value && !query.search) setOpen(false);
  };
  const tabs = selection ?? <div className="status-tabs" role="radiogroup" aria-label={messages.status} ref={tabsRef}>
    {statusTabs.filter(([value]) => showsTab(value, status, narrowed, counts)).map(([value, label]) => {
      // Open and Pending always say how many they hold, the tab on screen too; a zero says nothing.
      const count = (value === 'open' || value === 'pending' || value === status) ? counts?.[value] : undefined;
      return <label key={value}><input type="radio" name="status" value={value} checked={status === value} onChange={() => setStatus(value)} /><span>{label}</span>
        {count ? <span className="tab-count" aria-hidden="true">{count}</span> : null}</label>;
    })}
  </div>;
  return (
    // A search region, not a form: the saved views menu inside it has forms of its own, and a form inside a
    // form makes the browser submit the outer one as a page load.
    // The Filter menu keeps its place among the children in both layouts, so it stays open (and keeps the focus)
    // when its first tick turns the search row on.
    <div className="inbox-filters" role="search" aria-label={messages.inbox} data-search-row={searchRow || undefined}
      onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) closeIfEmpty(); }}>
      {!searchRow && tabs}
      <div className="filter-bar">
        {searchRow && <label className="search-field"><SearchIcon />
          {/* Clear filters starts the box afresh, a search still waiting to be sent included. */}
          <DraftInput key={focusSignal} inputRef={search} name="search" type="search" aria-label={messages.searchConversations} placeholder={messages.searchConversations} maxLength={200} autoFocus={focusSearch}
            value={query.search ?? ''} delay={typingDelayMs} onCommit={onSearch} onFocus={() => setOpen(true)}
            onKeyDown={(event) => {
              if (event.key !== 'Escape' || event.currentTarget.value || query.search || filtered) return;
              event.preventDefault();
              setOpen(false);
              setClosedByKey((count) => count + 1);
            }} /></label>}
        <FilterBarPart {...part} sortFirst={!searchRow} />
        {/* The saved views sit beside the search box; at rest they are listed in the sidebar. */}
        {searchRow && views}
      </div>
      {searchRow && tabs}
      {filtered && <FilterChipsPart {...part} />}
    </div>
  );
}
