import { useContext, useEffect, useRef, useState } from 'react';
import { QueryClientContext } from '@tanstack/react-query';
import { channelsApi } from '../../api/channels';
import type { ChannelList } from '../../api/channels';
import { resultsApi } from '../../api/results';
import type { Results } from '../../api/results';
import { knownChannels } from '../../api/schemas';
import type { Conversation, Tag, TeamMember } from '../../api/schemas';
import { messages } from '../../messages';
import { FilterIcon, XIcon } from '../../ui/icons';
import { zonedDay } from '../../lib/zoned-day';
import { listOf, withoutValue } from '../../lib/inbox-query-edit';
import type { InboxQuery, InboxQueryKey } from '../../lib/inbox-query';
import { trackFilterChanges } from './filter-analytics';
import { channelLabel, filterMessages as text } from './filter-messages';

// The Filter menu, the Sort menu and the chips of the active filters. They load on their own chunk, right
// after the first screen (inbox-filters.tsx).

export interface FilterPartProps {
  query: InboxQuery;
  onChange?: (query: InboxQuery) => void;
  tags: Tag[];
  agents: TeamMember[];
  timezone: string;
  conversations?: Conversation[];
  // The signed-in agent: listed as Me, never a second time under their name.
  currentAgentId?: string;
}

const priorities = ['urgent', 'high', 'normal', 'low'] as const;
// The known values of the contract's ConversationChannel, named by the registry or as an origin; a chat
// channel is chosen by itself (channel_id).
const channels = knownChannels;
const dateFilters = [['created', 'created_after', 'created_before'], ['updated', 'updated_after', 'updated_before']] as const;

// How a conversation started without a channel, offered in every workspace.
const origins: readonly string[] = ['imported', 'agent'];
const offeredSorts: readonly string[] = ['updated_desc', 'waiting_longest', 'priority'];

// The day before an exclusive end is the last day the person picked.
function lastDay(before: string, timeZone: string): string {
  return zonedDay(new Date(new Date(before).getTime() - 1).toISOString(), timeZone);
}

// Filters counted on the Filter button; status, sort and search have their own controls, and a date range
// counts once. Source, organization and the date ranges are set only by a link and show as chips.
const menuKeys: InboxQueryKey[] = ['priority', 'assignee', 'tag', 'contact', 'organization', 'awaiting', 'has_pending_draft', 'delivery_failed',
  'source', 'channel', 'channel_id', 'created_after', 'updated_after'];
const has = (query: InboxQuery, key: InboxQueryKey) => query[key] !== undefined && query[key] !== '';

// useChannelList reads the workspace's channels, removed ones too, once it is wanted: to offer their types
// and choose one channel in the menu, and to name the chip of one. Through the app's query cache (Settings reloads it);
// a part rendered without one reads them itself.
function useChannelList(wanted: boolean): ChannelList | undefined {
  const client = useContext(QueryClientContext);
  const [list, setList] = useState<ChannelList>();
  useEffect(() => {
    if (!wanted) return undefined;
    let live = true;
    const load = client ? client.fetchQuery({ queryKey: ['channels', 'all'], queryFn: channelsApi.listAll, staleTime: 30_000 }) : channelsApi.listAll();
    load.then((value) => { if (live) setList(value); }, () => {});
    return () => { live = false; };
  }, [client, wanted]);
  return list;
}

type ListFilter = 'priority' | 'assignee' | 'tag' | 'channel' | 'channel_id';

// changeOf replaces the filters (reporting what kind changed) and sets one of them.
function changeOf({ query, onChange }: FilterPartProps) {
  const change = (next: InboxQuery) => { trackFilterChanges(query, next); onChange?.(next); };
  const set = (key: InboxQueryKey, value: string | boolean | undefined) => {
    const next: InboxQuery = { ...query };
    if (value === undefined || value === '') delete next[key];
    else (next as Record<string, string | boolean>)[key] = value;
    if (!next.tag) delete next.tag_match;
    change(next);
  };
  return { change, set };
}

export function FilterBar(props: FilterPartProps) {
  const { query } = props;
  const { set } = changeOf(props);
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
  const sort = query.sort ?? 'updated_desc';
  const active = menuKeys.filter((key) => has(query, key)).length
    + (!has(query, 'created_after') && has(query, 'created_before') ? 1 : 0) + (!has(query, 'updated_after') && has(query, 'updated_before') ? 1 : 0);
  return (
    <>
      <details className="filter-menu" ref={menu} onKeyDown={closeOnEscape} onBlur={closeOnFocusOut} onToggle={(event) => { if (event.currentTarget.open) setOpened(true); }}>
        <summary><FilterIcon /><span>{messages.filter}</span>
          {active > 0 && <span className="filter-count" aria-label={messages.filtersActive(active)}>{active}</span>}</summary>
        <div className="filter-popover" role="group" aria-label={messages.filter}><FilterPanel {...props} opened={opened} /></div>
      </details>
      <select className="sort-field" aria-label={messages.inboxSort} value={sort} onChange={(event) => set('sort', event.currentTarget.value === 'updated_desc' ? undefined : event.currentTarget.value)}>
        {Object.entries(text.sorts).filter(([value]) => offeredSorts.includes(value) || value === sort).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select>
    </>
  );
}

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
      <fieldset><legend>{text.tags}</legend>
        {tags.length === 0 ? <p className="filter-hint">{text.noTags}</p> : tags.map((tag) => check('tag', tag.id, tag.name))}
        {(chosenTags.length > 1 || query.tag_match === 'all') && <label className="check-row filter-match"><input type="checkbox" checked={query.tag_match === 'all'}
          onChange={(event) => set('tag_match', event.currentTarget.checked ? 'all' : undefined)} /><span>{text.matchAllTags}</span></label>}
      </fieldset>
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

// Metrics counted on the conversations created a week earlier (docs/user/results.md).
const resolvedMetrics = new Set(['resolution', 'unresolved', 'without_resolve_time', 'reopened', 'handle']);

// A number of the results page: `<snapshot id>:<window>.<metric>`. With the page's numbers at hand (they
// are cached from Results, or read once) the chip says which side it counts and the days it covers.
export function resultLabel(result: string, results: Results | undefined, timeZone: string): string {
  const [snapshot, key] = result.split(':');
  const [window, metric] = (key ?? '').split('.');
  const metricName = text.resultMetrics[metric] ?? metric;
  const side = results?.current?.id === snapshot ? results.current : results?.baseline?.id === snapshot ? results.baseline : undefined;
  const numbers = side?.windows[window as '30d' | '7d'];
  if (!side || !numbers) return text.countedIn(metricName, text.resultWindows[window] ?? window);
  const name = side === results?.current ? 'Colliedesk' : side.source?.startsWith('helpscout:') ? 'Help Scout' : 'Freshdesk';
  const span = resolvedMetrics.has(metric) ? numbers.resolved_created : numbers.created;
  const day = (value: string) => zonedDayLabel(value, timeZone);
  return text.countedInSide(metricName, name, `${day(span.from)} – ${day(span.to)}`);
}

function zonedDayLabel(value: string, timeZone: string): string {
  try {
    return new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', year: 'numeric', timeZone }).format(new Date(value));
  } catch {
    return new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC' }).format(new Date(value));
  }
}

interface Chip {
  key: InboxQueryKey;
  value?: string;
  label: string;
}

export function FilterChips(props: FilterPartProps) {
  const { query, tags, agents, timezone, conversations } = props;
  const { change: onChange } = changeOf(props);
  // The numbers of the results page name the side and days of a result chip: cached from Results, or
  // read once for a link opened directly.
  const cached = useContext(QueryClientContext)?.getQueryData<Results>(['results']);
  const [fetched, setFetched] = useState<Results>();
  useEffect(() => {
    if (!query.result || cached) return undefined;
    let live = true;
    resultsApi.get().then((value) => { if (live) setFetched(value); }, () => {});
    return () => { live = false; };
  }, [query.result, cached]);
  const results = { data: cached ?? fetched };
  const channelList = useChannelList(Boolean(query.channel_id));
  const channelName = (id: string) => {
    const channel = channelList?.items.find((item) => item.id === id);
    return !channel ? text.unknownChannel : channel.deleted_at ? text.removedChannel(channel.name) : channel.name;
  };
  const agentName = (id: string) => {
    const agent = agents.find((item) => item.id === id);
    return agent ? agent.display_name || agent.email : text.unknownAgent;
  };
  const assigneeLabel = (value: string) => (value === 'me' ? messages.assignToMe : value === 'unassigned' ? messages.unassigned : value === 'anyone' ? text.anyoneAssigned : agentName(value));
  const tagName = (id: string) => tags.find((tag) => tag.id === id)?.name ?? text.unknownTag;
  // In the order of the Filter menu.
  const chips: Chip[] = [
    ...listOf(query, 'priority').map((value) => ({ key: 'priority' as const, value, label: `${text.priority}: ${messages.priorities[value as keyof typeof messages.priorities] ?? value}` })),
    ...listOf(query, 'assignee').map((value) => ({ key: 'assignee' as const, value, label: `${text.assignee}: ${assigneeLabel(value)}` })),
    ...listOf(query, 'tag').map((value) => ({ key: 'tag' as const, value, label: `${text.tags}: ${tagName(value)}` })),
    ...(query.contact ? [{ key: 'contact' as const, label: `${text.contact}: ${conversations?.find((item) => item.contact_id === query.contact)?.contact_name ?? text.selectedContact}` }] : []),
    ...(query.awaiting === 'team' || query.awaiting === 'customer' ? [{ key: 'awaiting' as const, label: text.awaitingOptions[query.awaiting] }] : []),
    ...(query.has_pending_draft !== undefined ? [{ key: 'has_pending_draft' as const, label: query.has_pending_draft ? text.hasPendingDraft : text.noPendingDraft }] : []),
    ...(query.delivery_failed !== undefined ? [{ key: 'delivery_failed' as const, label: query.delivery_failed ? text.deliveryFailed : text.noDeliveryFailed }] : []),
    ...listOf(query, 'channel').map((value) => ({ key: 'channel' as const, value, label: `${text.channel}: ${channelLabel(value)}` })),
    ...listOf(query, 'channel_id').map((value) => ({ key: 'channel_id' as const, value, label: `${text.channel}: ${channelName(value)}` })),
    ...(query.source === 'imported' || query.source === 'native' ? [{ key: 'source' as const, label: `${text.source}: ${text.sources[query.source]}` }] : []),
    ...(query.organization ? [{ key: 'organization' as const, label: `${text.organization}: ${query.organization}` }] : []),
    ...(query.ref ? [{ key: 'ref' as const, label: `${text.reference}: ${query.ref}` }] : []),
    ...(query.result ? [{ key: 'result' as const, label: resultLabel(query.result, results.data, timezone) }] : []),
    ...dateFilters.flatMap(([name, after, before]) => (query[after] || query[before]
      ? [{ key: after, label: `${text[name]}: ${text.dateRange(query[after] ? zonedDay(query[after], timezone) : undefined, query[before] ? lastDay(query[before], timezone) : undefined)}` }]
      : [])),
  ];
  if (chips.length === 0) return null;
  const removeChip = (chip: Chip) => {
    let next = withoutValue(query, chip.key, chip.value);
    // A date chip stands for both ends of its range.
    for (const [, after, before] of dateFilters) if (chip.key === after) next = withoutValue(next, before);
    onChange?.(next);
  };
  return (
    <div className="filter-chips">
      <ul aria-label={text.active}>
        {chips.map((chip) => (
          <li key={`${chip.key}:${chip.value ?? ''}`}>{chip.label}
            <button type="button" className="chip-remove" aria-label={text.remove(chip.label)} onClick={() => removeChip(chip)}><XIcon /></button></li>))}
      </ul>
      <button type="button" className="link-button" onClick={() => onChange?.({})}>{text.clearAll}</button>
      {query.result && <a href={query.result.includes(':7d.') ? '/results?window=7d' : '/results'} className="link-button">{text.backToResults}</a>}
      {query.result && <p className="filter-chips-note">{text.anyStatus}</p>}
    </div>
  );
}
