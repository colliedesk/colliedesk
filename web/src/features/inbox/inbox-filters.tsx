import { useEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { messages } from '../../messages';
import type { Conversation, Tag, TeamMember } from '../../api/schemas';
import { SearchIcon } from '../../ui/icons';
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
  focusSearch?: boolean;
  // Bumped to move focus to the search box, for example after the list's Clear filters.
  focusSignal?: number;
  // The saved views menu, which sits in the same bar.
  views?: ReactNode;
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
  return part.status === 'error' ? <PartUnavailable />
    : <><button type="button" className="filter-menu-pending" disabled>{messages.filter}</button><select className="sort-field" disabled aria-label={messages.inboxSort} /></>;
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

// Search and the Filter menu sit above the status tabs, in the same order on screen and for Tab.
// Status is the view an agent switches most, so it is a row of tabs; everything else shares one
// Filter menu that counts what is on, and each active filter shows as a chip that removes it.
export function InboxFilters({ query, onChange, tags, agents = [], timezone = 'UTC', conversations, focusSearch = false, focusSignal = 0, views }: InboxFiltersProps) {
  const search = useRef<HTMLInputElement>(null);
  const setStatus = (value: string) => {
    const next: InboxQuery = { ...query, status: value };
    // Open with no status picked stays the default address.
    if (value === defaultStatus && query.status === undefined) delete next.status;
    onChange?.(next);
  };
  const onSearch = (value: string) => {
    const next: InboxQuery = { ...query, search: value };
    if (!value) delete next.search;
    // Search looks through every status unless the agent chose one: an imported ticket is usually closed.
    if (value && query.status === undefined) next.status = allStatuses;
    onChange?.(next);
  };
  useEffect(() => {
    if (focusSignal > 0) search.current?.focus();
  }, [focusSignal]);
  const part = { query, onChange, tags, agents, timezone, conversations };
  const status = query.status ?? defaultStatus;
  return (
    // A search region, not a form: the saved views menu inside it has forms of its own, and a form inside a
    // form makes the browser submit the outer one as a page load.
    <div className="inbox-filters" role="search" aria-label={messages.inbox}>
      <div className="filter-bar">
        <label className="search-field"><SearchIcon />
          {/* Clear filters starts the box afresh, a search still waiting to be sent included. */}
          <DraftInput key={focusSignal} inputRef={search} name="search" type="search" aria-label={messages.searchConversations} placeholder={messages.searchConversations} maxLength={200} autoFocus={focusSearch}
            value={query.search ?? ''} delay={typingDelayMs} onCommit={onSearch} /></label>
        <FilterBarPart {...part} />
        {views}
      </div>
      <div className="status-tabs" role="radiogroup" aria-label={messages.status}>
        {statusTabs.map(([value, label]) => (
          <label key={value}><input type="radio" name="status" value={value} checked={status === value} onChange={() => setStatus(value)} /><span>{label}</span></label>
        ))}
      </div>
      {Object.keys(query).some((key) => !ownControls.includes(key)) && <FilterChipsPart {...part} />}
    </div>
  );
}
