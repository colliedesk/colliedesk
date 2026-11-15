import { useEffect, useRef, useState } from 'react';
import { knownChannels } from '../../api/schemas';
import { messages } from '../../messages';
import { FilterIcon, SortIcon } from '../../ui/icons';
import { listOf } from '../../lib/inbox-query-edit';
import type { InboxQuery, InboxQueryKey } from '../../lib/inbox-query';
import { defaultSort, shownSort } from '../../lib/inbox-query';
import { channelLabel, filterMessages as text } from './filter-messages';
import { slaOf } from '../../lib/sla';
import { useHolds } from '../../lib/permissions-context';
import { changeOf, useChannelList } from './filter-part';
import type { FilterPartProps } from './filter-part';

// The Filter menu, the Sort menu and the chips of the active filters. They load on their own chunk, right
// after the first screen (inbox-filters.tsx).
export type { FilterPartProps } from './filter-part';
export { FilterChips, resultLabel } from './filter-chips';

const priorities = ['urgent', 'high', 'normal', 'low'] as const;
// The known values of the contract's ConversationChannel, named by the registry or as an origin; a chat
// channel is chosen by itself (channel_id).
const channels = knownChannels;

// How a conversation started without a channel, offered in every workspace.
const origins: readonly string[] = ['imported', 'agent'];
const offeredSorts: readonly string[] = ['due', 'updated_desc', 'waiting_longest', 'priority'];

// Filters counted on the Filter button; status, sort and search have their own controls, and a date range
// counts once. Source, organization and the date ranges are set only by a link and show as chips.
const menuKeys: InboxQueryKey[] = ['priority', 'assignee', 'tag', 'sla', 'contact', 'organization', 'awaiting', 'has_pending_draft', 'delivery_failed',
  'source', 'channel', 'channel_id', 'created_after', 'updated_after'];
const has = (query: InboxQuery, key: InboxQueryKey) => query[key] !== undefined && query[key] !== '';

// activeFilters counts the filters that are on, the channel the title names left out.
function activeFilters(query: InboxQuery, scopeChannel?: string): number {
  return menuKeys.filter((key) => has(query, key) && !(key === 'channel_id' && scopeChannel)).length
    + (!has(query, 'created_after') && has(query, 'created_before') ? 1 : 0) + (!has(query, 'updated_after') && has(query, 'updated_before') ? 1 : 0);
}

// SortPick names the order the list is in beside its icon; the native list of orders opens over both.
export function SortPick(props: FilterPartProps) {
  const { set } = changeOf(props);
  // The order the default gives (Waiting longest while reply targets are off) stands in for Due first, and
  // picking it returns to the default, which follows the targets once they are on.
  const shownDefault = props.dueSort ?? defaultSort;
  const sort = shownSort(props.query, props.dueSort);
  const offered = shownDefault === defaultSort ? offeredSorts : offeredSorts.filter((value) => value !== defaultSort);
  return <label className="sort-pick"><SortIcon /><span className="sort-label" aria-hidden="true">{messages.sorts[sort] ?? sort}</span>
    <select className="sort-field" aria-label={messages.inboxSort} value={sort} onChange={(event) => set('sort', [defaultSort, shownDefault].includes(event.currentTarget.value) ? undefined : event.currentTarget.value)}>
      {offered.concat(offered.includes(sort) ? [] : [sort]).map((value) => <option key={value} value={value}>{messages.sorts[value] ?? value}</option>)}
    </select></label>;
}

// FilterMenu holds every filter but status, order and search. With picks on, its icon turns the accent colour and
// says how many beside it.
export function FilterMenu(props: FilterPartProps) {
  const menu = useRef<HTMLDetailsElement>(null);
  // The channels are read the first time the menu opens.
  const [opened, setOpened] = useState(false);
  useEffect(() => {
    const close = (event: PointerEvent) => { if (menu.current?.open && !menu.current.contains(event.target as Node)) menu.current.open = false; };
    document.addEventListener('pointerdown', close);
    return () => document.removeEventListener('pointerdown', close);
  }, []);
  // Keyboard users leave the menu with Tab; it closes like it does on an outside click.
  const closeOnFocusOut = (event: React.FocusEvent<HTMLDetailsElement>) => {
    if (menu.current?.open && !menu.current.contains(event.relatedTarget as Node | null)) menu.current.open = false;
  };
  const closeOnEscape = (event: React.KeyboardEvent<HTMLDetailsElement>) => {
    if (event.key !== 'Escape' || !menu.current?.open) return;
    menu.current.open = false;
    menu.current.querySelector('summary')?.focus();
  };
  const active = activeFilters(props.query, props.scopeChannel);
  return (
    <details className="filter-menu" ref={menu} onKeyDown={closeOnEscape} onBlur={closeOnFocusOut} onToggle={(event) => { if (event.currentTarget.open) setOpened(true); }}>
      <summary data-tip={messages.filter} data-active={active > 0 || undefined}><FilterIcon /><span className="sr-only">{messages.filter}</span>
        {active > 0 && <span className="filter-count" aria-label={messages.filtersActive(active)}>{active}</span>}</summary>
      <div className="filter-popover" role="group" aria-label={messages.filter}><FilterPanel {...props} opened={opened} /></div>
    </details>
  );
}

type ListFilter = 'priority' | 'assignee' | 'tag' | 'channel' | 'channel_id' | 'sla';

function FilterPanel(props: FilterPartProps & { opened: boolean }) {
  const { query, tags, agents, currentAgentId, opened } = props;
  const { set } = changeOf(props);
  // A type with two or more live channels offers each of them; one chosen before stays listed.
  const list = useChannelList(opened || Boolean(query.channel_id));
  const chosenChannels = listOf(query, 'channel_id');
  const channelGroups = (list?.types ?? []).map(({ type }) => ({ type, items: (list?.items ?? []).filter((item) => item.type === type && (!item.deleted_at || chosenChannels.includes(item.id))) }))
    .filter((group) => group.items.filter((item) => !item.deleted_at).length > 1);
  const toggle = (key: ListFilter, value: string, on: boolean) => {
    const values = listOf(query, key).filter((item) => item !== value);
    if (on) values.push(value);
    set(key, values.join(','));
  };
  const chosenTags = listOf(query, 'tag');
  // A role that may not read the tag list is offered no tag to filter by.
  const readsTags = useHolds()('tags:read');
  // A channel type is offered once the workspace has (or had) a channel of it; a problem report reaches only
  // the operator's workspace. A value chosen before stays listed, so it can be unticked.
  const offeredChannels = channels.filter((value) => origins.includes(value) || listOf(query, 'channel').includes(value) || list?.items.some((item) => item.type === value));
  const check = (key: ListFilter, value: string, label: string) => (
    <label key={value} className="check-row"><input type="checkbox" checked={listOf(query, key).includes(value)} onChange={(event) => toggle(key, value, event.currentTarget.checked)} /><span>{label}</span></label>
  );
  return (
    <>
      <fieldset><legend>{text.priority}</legend>{priorities.map((value) => check('priority', value, messages.priorities[value]))}</fieldset>
      <fieldset><legend>{text.assignee}</legend>
        {check('assignee', 'me', messages.assignToMe)}
        {agents.filter((agent) => !agent.deactivated_at && agent.id !== currentAgentId).map((agent) => check('assignee', agent.id, agent.display_name || agent.email))}
        {check('assignee', 'unassigned', messages.unassigned)}
      </fieldset>
      {readsTags && <fieldset><legend>{text.tags}</legend>
        {tags.length === 0 ? <p className="filter-hint">{text.noTags}</p> : tags.map((tag) => check('tag', tag.id, tag.name))}
        {(chosenTags.length > 1 || query.tag_match === 'all') && <label className="check-row filter-match"><input type="checkbox" checked={query.tag_match === 'all'}
          onChange={(event) => set('tag_match', event.currentTarget.checked ? 'all' : undefined)} /><span>{text.matchAllTags}</span></label>}
      </fieldset>}
      {/* Offered once a listed conversation has targets (they are on), or while the filter is set. */}
      {(listOf(query, 'sla').length > 0 || props.conversations?.some((item) => slaOf(item))) && <fieldset><legend>{text.targets}</legend>
        {check('sla', 'overdue', text.targetStates.overdue)}{check('sla', 'due_soon', text.targetStates.due_soon)}</fieldset>}
      <fieldset><legend>{text.attention}</legend>
        <label className="check-row"><input type="checkbox" checked={query.awaiting === 'team'} onChange={(event) => set('awaiting', event.currentTarget.checked ? 'team' : undefined)} /><span>{text.awaitingOptions.team}</span></label>
        <label className="check-row"><input type="checkbox" checked={query.delivery_failed === true} onChange={(event) => set('delivery_failed', event.currentTarget.checked || undefined)} /><span>{text.deliveryFailed}</span></label>
      </fieldset>
      <fieldset><legend>{text.channel}</legend>{offeredChannels.map((value) => check('channel', value, channelLabel(value)))}</fieldset>
      {channelGroups.map(({ type, items }) => <fieldset key={type}><legend>{text.channelsOf(channelLabel(type))}</legend>
        {items.map((item) => check('channel_id', item.id, item.deleted_at ? text.removedChannel(item.name) : item.name))}</fieldset>)}
    </>
  );
}
