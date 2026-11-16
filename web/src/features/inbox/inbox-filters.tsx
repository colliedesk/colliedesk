import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { messages } from '../../messages';
import type { Conversation, SavedView, Tag, TeamMember } from '../../api/schemas';
import { FilterIcon, SearchIcon, SortIcon } from '../../ui/icons';
import { allStatuses, defaultStatus, shownSort } from '../../lib/inbox-query';
import type { InboxQuery } from '../../lib/inbox-query';
import { activeFilterCount, onView, scopedChannel } from '../../lib/inbox-scope';
import type { FilterPartProps } from './filter-panel';
import { lazyPart, PartUnavailable } from '../../ui/lazy-part';
import { ListSearchField } from './list-search-field';
import { StatusPicker } from './status-picker';

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
  // Bumped to move focus to the search field, for example after the list's Clear filters.
  focusSignal?: number;
  // The saved views menu, offered beside Filter while the list is narrowed.
  views?: ReactNode;
  // Select all and the bulk actions, in the place of the status and the order while rows are ticked.
  selection?: ReactNode;
  // How many conversations each status holds under the other filters (status_counts, all as their sum).
  counts?: Partial<Record<string, number>>;
  // The order Due first gives the list (FilterPartProps.dueSort).
  dueSort?: string;
  // The workspace's channels and saved views, as the sidebar lists them: they name the place the list is in. Undefined
  // while they load, so a channel's title waits for its name instead of saying something else first.
  channels?: Array<{ id: string; name: string }>;
  savedViews?: Array<Pick<SavedView, 'id' | 'name' | 'filters'>>;
}

// The Filter menu, the Sort menu and the chips load on their own chunk (filter-panel.tsx), asked for as soon as the
// inbox renders, so the first bundle does not carry them. Until they arrive, the title, the search and the status
// work, and inert controls of the same size (the order already named) hold their places.
export const filterPart = lazyPart(() => import('./filter-panel'));
// A search is sent once the person pauses this long, so a word is one request, not one per key.
export const typingDelayMs = 100;

function FilterMenuPart(props: FilterPartProps) {
  const part = filterPart.use();
  if (part.status === 'ready') return <part.module.FilterMenu {...props} />;
  if (part.status === 'error') return <PartUnavailable />;
  // The count and the accent show already, from the address, so nothing moves when the menu arrives.
  const active = activeFilterCount(props.query, props.scopeChannel);
  return <button type="button" className="filter-menu-pending icon-button" disabled data-active={active > 0 || undefined}><FilterIcon /><span className="sr-only">{messages.filter}</span>
    {active > 0 && <span className="filter-count" aria-label={messages.filtersActive(active)}>{active}</span>}</button>;
}

function SortPickPart(props: FilterPartProps) {
  const part = filterPart.use();
  if (part.status === 'ready') return <part.module.SortPick {...props} />;
  if (part.status === 'error') return null;
  const sort = shownSort(props.query, props.dueSort);
  return <span className="sort-pick"><SortIcon /><span className="sort-label" aria-hidden="true">{messages.sorts[sort] ?? sort}</span>
    <select className="sort-field" disabled aria-label={messages.inboxSort} /></span>;
}

function FilterChipsPart(props: FilterPartProps) {
  const part = filterPart.use();
  // Until the chips arrive, an empty row of their height holds their place, so the list does not jump.
  if (part.status === 'ready') return <part.module.FilterChips {...props} />;
  return part.status === 'error' ? null : <div className="filter-chips filter-chips-pending" aria-hidden="true" />;
}

// Keys with controls of their own; any other filter shows as a chip.
const ownControls = ['status', 'sort', 'search'];

// placeOf names where the list is: the saved view on screen, else the one channel it is narrowed to, else every channel.
export function placeOf(query: InboxQuery, channels: InboxFiltersProps['channels'], views: InboxFiltersProps['savedViews']) {
  const view = views?.find((item) => onView(query, item.filters));
  if (view) return { title: view.name, scope: messages.searchIn(view.name), view };
  const id = scopedChannel(query);
  if (!id) return { title: messages.allChannels, scope: messages.searchInAllChannels };
  // Blank only while the channel names load; a list that failed or lacks the channel still says it is one channel.
  if (!channels) return { title: '', scope: '', channel: id };
  const name = channels.find((item) => item.id === id)?.name;
  return { title: name ?? messages.unknownChannel, scope: name ? messages.searchIn(name) : messages.searchInThisChannel, channel: id };
}

// The list header: the place the list is in as its title, with Search and Filter (and the saved views while the list is
// narrowed); under it the status and the order, then a chip for each filter that is on. Search turns the title into a
// field (from its icon, the app's search through focusSignal, or ?search= in a link). The order on screen is the order for Tab.
export function InboxFilters({ query, onChange, tags, agents = [], timezone = 'UTC', conversations, currentAgentId, focusSearch = false, focusSignal = 0, views, selection, counts, dueSort, channels, savedViews }: InboxFiltersProps) {
  const search = useRef<HTMLInputElement>(null);
  const searchButton = useRef<HTMLButtonElement>(null);
  // The field shows while the person uses it: opened by its icon, the app's Search or Clear filters, typed in, or
  // focused. It closes again once it is empty and the focus leaves the header, on Escape, or on its clear button.
  const [open, setOpen] = useState(focusSearch || focusSignal > 0);
  const [seenSignal, setSeenSignal] = useState(focusSignal);
  const [focusField, setFocusField] = useState(focusSignal);
  if (focusSignal !== seenSignal) {
    setSeenSignal(focusSignal);
    setOpen(true);
    setFocusField((count) => count + 1);
  }
  // Closing the field from the keyboard leaves the focus on the search icon that takes its place, once it is there: a
  // cleared search leaves the address a moment later.
  const [refocus, setRefocus] = useState(false);
  useEffect(() => { if (focusField > 0) search.current?.focus(); }, [focusField]);
  const setStatus = (value: string) => {
    const next: InboxQuery = { ...query, status: value };
    // Open with no status picked stays the default address.
    if (value === defaultStatus && query.status === undefined) delete next.status;
    onChange?.(next);
  };
  const place = placeOf(query, channels, savedViews);
  const onSearch = (value: string) => {
    setOpen(true);
    const next: InboxQuery = { ...query, search: value };
    if (!value) delete next.search;
    // Search looks through every status unless the agent chose one: an imported ticket is usually closed. A saved
    // view's own status counts as chosen, so the search stays inside the view.
    if (value && query.status === undefined && !place.view?.filters.status?.length) next.status = allStatuses;
    onChange?.(next);
  };
  const part = { query, onChange, tags, agents, timezone, conversations, currentAgentId, dueSort, scopeChannel: place.channel };
  // A filter other than the channel the title names: it shows a chip and the saved views.
  const filtered = Object.keys(query).some((key) => !ownControls.includes(key) && !(key === 'channel_id' && place.channel));
  const searching = open || Boolean(query.search);
  useEffect(() => {
    if (!refocus || searching) return;
    searchButton.current?.focus();
    setRefocus(false);
  }, [refocus, searching]);
  const closeField = () => { setOpen(false); setRefocus(true); };
  const clearSearch = () => {
    const next: InboxQuery = { ...query };
    delete next.search;
    if (query.search) onChange?.(next);
    closeField();
  };
  return (
    // A search region, not a form: the saved views menu inside it has forms of its own, and a form inside a
    // form makes the browser submit the outer one as a page load.
    // Filter keeps its place among the children whether the title or the field shows, so it stays open (and keeps the
    // focus) when its first tick brings the saved views in.
    <div className="inbox-filters" role="search" aria-label={messages.inbox} data-searching={searching || undefined}
      onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null) && !search.current?.value && !query.search) setOpen(false); }}>
      <div className="list-title-row">
        {searching
          // Clear filters starts the field afresh, a search still waiting to be sent included.
          ? <ListSearchField key={focusSignal} inputRef={search} value={query.search ?? ''} delay={typingDelayMs} onCommit={onSearch} onFocus={() => setOpen(true)}
              autoFocus={focusSearch} scope={place.scope} onClear={clearSearch}
              onKeyDown={(event) => {
                if (event.key !== 'Escape' || event.currentTarget.value || query.search) return;
                event.preventDefault();
                closeField();
              }} />
          : <><h2 className="list-scope">{place.title}</h2>
            <button ref={searchButton} type="button" className="icon-button list-search-button" aria-label={messages.search} data-tip={messages.search}
              onClick={() => { setOpen(true); setFocusField((count) => count + 1); }}><SearchIcon /></button></>}
        <FilterMenuPart {...part} />
        {(searching || filtered) && views}
      </div>
      {selection ?? <div className="list-controls">
        <StatusPicker status={query.status ?? defaultStatus} counts={counts} onPick={setStatus} />
        <SortPickPart {...part} />
      </div>}
      {filtered && <FilterChipsPart {...part} />}
    </div>
  );
}
